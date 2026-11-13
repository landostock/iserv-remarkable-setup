package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type mappingsView struct {
	Sources  []Source `json:"sources"`
	Revision string   `json:"revision"`
}

func mappingRevision(sources []Source) string {
	raw, _ := json.Marshal(sources)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func validateMappingSources(sources []Source) error {
	cfg := defaultConfig()
	cfg.AdminPassword = "validation-only-password"
	cfg.Sources = sources
	if err := validConfig(cfg); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, source := range sources {
		if len([]rune(source.Name)) > 60 || source.Name != strings.TrimSpace(source.Name) || source.IServPath != strings.Trim(source.IServPath, "/") || seen[source.IServPath] {
			return fmt.Errorf("Zuordnungen brauchen kurze eindeutige Namen und unterschiedliche IServ-Quellen")
		}
		seen[source.IServPath] = true
	}
	return nil
}

func sourcesFromSettings(settings map[string]any) ([]Source, error) {
	sync, ok := settings["sync"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("Dashboard-Einstellungen sind unvollständig")
	}
	raw, err := json.Marshal(sync["sources"])
	if err != nil {
		return nil, err
	}
	var sources []Source
	if json.Unmarshal(raw, &sources) != nil {
		return nil, fmt.Errorf("Ordnerzuordnungen konnten nicht gelesen werden")
	}
	return sources, validateMappingSources(sources)
}

func readMappingsState(base, password string, request adminRequestFunc) (dashboardState, mappingsView, error) {
	var state dashboardState
	raw, status, err := request(base, password, http.MethodGet, "/api/admin/settings", nil)
	if err != nil || status != http.StatusOK {
		return state, mappingsView{}, fmt.Errorf("Dashboard-Anmeldung oder Verbindung fehlgeschlagen")
	}
	if json.Unmarshal(raw, &state) != nil {
		return state, mappingsView{}, fmt.Errorf("Dashboard-Einstellungen sind unvollständig")
	}
	sources, err := sourcesFromSettings(state.Settings)
	return state, mappingsView{Sources: sources, Revision: mappingRevision(sources)}, err
}

func saveMappings(base, password, revision string, sources []Source, request adminRequestFunc) error {
	if err := validateMappingSources(sources); err != nil {
		return err
	}
	current, original, err := readMappingsState(base, password, request)
	if err != nil {
		return err
	}
	if revision != original.Revision {
		return fmt.Errorf("Die Zuordnungen wurden inzwischen geändert. Lade sie erneut und prüfe deine Auswahl")
	}
	enabled, ok := current.Settings["enabled"].(bool)
	if !ok || enabled {
		return fmt.Errorf("Pausiere zuerst die Automatik im Dashboard, bevor du Ordnerzuordnungen änderst")
	}
	switch current.Status.MachineState {
	case "starting", "started", "running", "stopping":
		return fmt.Errorf("Warte, bis der aktuelle Synchronisationslauf abgeschlossen ist")
	}
	settings, err := cloneDashboardSettings(current.Settings)
	if err != nil {
		return err
	}
	rawSources, _ := json.Marshal(sources)
	var values []any
	if err := json.Unmarshal(rawSources, &values); err != nil {
		return err
	}
	settings["sync"].(map[string]any)["sources"] = values
	raw, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	response, status, err := request(base, password, http.MethodPut, "/api/admin/settings", raw)
	if err != nil || status != http.StatusOK {
		return fmt.Errorf("Ordnerzuordnungen konnten nicht gespeichert werden")
	}
	var accepted struct {
		Settings map[string]any `json:"settings"`
	}
	if json.Unmarshal(response, &accepted) != nil {
		return fmt.Errorf("Das Dashboard hat die gespeicherten Zuordnungen nicht bestätigt. Lade den Stand erneut")
	}
	confirmed, err := sourcesFromSettings(accepted.Settings)
	if err != nil || mappingRevision(confirmed) != mappingRevision(sources) {
		return fmt.Errorf("Das Dashboard hat die gespeicherten Zuordnungen nicht bestätigt. Lade den Stand erneut")
	}
	return nil
}

type mappingRequest struct {
	Password string   `json:"password"`
	Revision string   `json:"revision,omitempty"`
	Sources  []Source `json:"sources,omitempty"`
}

func (a *App) mappingSettingsIdle(w http.ResponseWriter, r *http.Request) bool {
	a.st.mu.RLock()
	busy := a.st.MaintenanceStatus == "running"
	a.st.mu.RUnlock()
	if busy {
		writeAPIError(w, r, 409, "Warte, bis die Wartungsaktion abgeschlossen ist", "Wait until maintenance has completed")
	}
	return !busy
}

func (a *App) mappingInput(w http.ResponseWriter, r *http.Request) (*installMeta, mappingRequest, bool) {
	var in mappingRequest
	if r.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return nil, in, false
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&in) != nil || decoder.Decode(new(any)) != io.EOF || !firstRealPasswordValid(in.Password) {
		writeAPIError(w, r, 400, "Gültiges Dashboard-Passwort erforderlich.", "A valid dashboard password is required.")
		return nil, in, false
	}
	meta := a.metaSnapshot()
	if meta == nil {
		writeAPIError(w, r, 409, "Installation ist nicht verbunden.", "Installation is not connected.")
		return nil, in, false
	}
	return meta, in, true
}

func (a *App) handleMappingsRead(w http.ResponseWriter, r *http.Request) {
	meta, in, ok := a.mappingInput(w, r)
	if !ok {
		return
	}
	_, view, err := readMappingsState(meta.DashboardURL, in.Password, adminRequest)
	if err != nil {
		writeAPIError(w, r, 400, "Dashboard-Anmeldung oder Verbindung fehlgeschlagen", "Dashboard sign-in or connection failed")
		return
	}
	writeJSON(w, 200, view)
}

func (a *App) handleMappingsSave(w http.ResponseWriter, r *http.Request) {
	meta, in, ok := a.mappingInput(w, r)
	if !ok {
		return
	}
	_, err := hex.DecodeString(in.Revision)
	if err != nil || len(in.Revision) != 64 || validateMappingSources(in.Sources) != nil {
		writeAPIError(w, r, 400, "Prüfe die Ordnerzuordnungen und lade bei Bedarf den gespeicherten Stand erneut", "Check the folder mappings and reload the saved state if needed")
		return
	}
	if !a.runMaintenance("mappings", func() error {
		a.settingsMu.Lock()
		defer a.settingsMu.Unlock()
		if a.loadFirstRealState(meta).Status == "running" {
			return fmt.Errorf("Warte, bis der aktuelle Synchronisationslauf abgeschlossen ist")
		}
		return saveMappings(meta.DashboardURL, in.Password, in.Revision, in.Sources, adminRequest)
	}) {
		writeAPIError(w, r, 409, "Es läuft bereits eine Wartungsaktion", "A maintenance action is already running")
		return
	}
	writeJSON(w, 202, map[string]any{"started": true, "revision": mappingRevision(in.Sources)})
}

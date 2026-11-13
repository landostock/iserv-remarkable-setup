package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const firstRealRunTimeout = 20 * time.Minute

type firstRealSyncState struct {
	Status    string              `json:"status"`
	AppName   string              `json:"app_name,omitempty"`
	StartedAt time.Time           `json:"started_at,omitempty"`
	UpdatedAt time.Time           `json:"updated_at,omitempty"`
	Error     string              `json:"error,omitempty"`
	Result    *verificationResult `json:"result,omitempty"`
}

var firstRealStartMu sync.Mutex

type bufferedResponseWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func newBufferedResponseWriter() *bufferedResponseWriter {
	return &bufferedResponseWriter{header: make(http.Header)}
}

func (w *bufferedResponseWriter) Header() http.Header { return w.header }
func (w *bufferedResponseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *bufferedResponseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(p)
}

func (a *App) rootHandler() http.Handler {
	base := a.routes()
	start := a.secure(a.handleFirstRealSyncStart)
	status := a.secure(a.handleFirstRealSyncStatus)
	enable := a.secure(a.handleEnableAutomaticSync)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/install/first-real/start":
			start(w, r)
			return
		case "/api/install/first-real/status":
			status(w, r)
			return
		case "/api/install/enable-automatic":
			enable(w, r)
			return
		}
		if r.URL.Path != "/" || r.Method != http.MethodGet {
			base.ServeHTTP(w, r)
			return
		}

		captured := newBufferedResponseWriter()
		base.ServeHTTP(captured, r)
		for key, values := range captured.header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		statusCode := captured.status
		if statusCode == 0 {
			statusCode = http.StatusOK
		}
		body := captured.body.Bytes()
		if statusCode == http.StatusOK && strings.Contains(strings.ToLower(w.Header().Get("Content-Type")), "text/html") {
			if enhanced, ok := injectFirstRealSyncUI(string(body)); ok {
				body = []byte(enhanced)
				w.Header().Del("Content-Length")
			}
		}
		w.WriteHeader(statusCode)
		_, _ = w.Write(body)
	})
}

func injectFirstRealSyncUI(page string) (string, bool) {
	index := strings.LastIndex(page, "</script>")
	if index < 0 || strings.Contains(page, "firstRealSyncPanel") {
		return page, false
	}
	return page[:index] + firstRealSyncUIJS + page[index:], true
}

const firstRealSyncUIJS = `
(function(){
  const done=document.querySelector("#done");
  const maintenanceSlot=document.querySelector("#maintenanceActivationSlot");
  if(!done||!maintenanceSlot||document.querySelector("#firstRealSyncPanel"))return;
  let maintenanceMode=false;
  const panel=document.createElement("div");
  panel.id="firstRealSyncPanel";
  panel.className="setting-card";
  panel.style.marginTop="14px";
  panel.innerHTML='<h3 id="firstRealTitle"></h3><p id="firstRealHint" class="muted"></p><label id="firstRealPasswordLabel" for="firstRealPassword"></label><input id="firstRealPassword" type="password" autocomplete="current-password"><div class="row" style="margin-top:10px"><button class="secondary" id="firstRealStart" type="button"></button><button class="primary" id="firstRealEnable" type="button" disabled></button></div><div id="firstRealStatus" class="statusbox" aria-live="polite"></div>';
  done.appendChild(panel);
  const q=s=>document.querySelector(s);
  const copy=(de,en)=>LANG==="en"?en:de;
  function renderFirstRealCopy(){
    q("#firstRealTitle").textContent=maintenanceMode
      ? copy("Sichere Freigabe abschließen","Complete safe activation")
      : copy("7. Begrenzten echten Lauf bestätigen","7. Confirm a limited real run");
    q("#firstRealHint").textContent=maintenanceMode
      ? copy("Hier schließt du die sichere Freigabe ab. Wurde der echte Lauf bereits bestätigt, kannst du ihn mit dem Dashboard-Passwort erkennen lassen und die Automatik aktivieren. Andernfalls starte zuerst den begrenzten echten Lauf.","Complete safe activation here. If the real run was already verified, enter the dashboard password to recognize it and enable automation. Otherwise, start the limited real run first.")
      : copy("Der Sicherheits-Test war ohne Upload. Als Nächstes kannst du bewusst einen echten manuellen Lauf starten. Er bleibt auf höchstens fünf Upload-Versuche begrenzt und die Automatik bleibt dabei AUS. Erst nach einem erfolgreichen Lauf kann sie hier aktiviert werden.","The safety test made no uploads. Next you can deliberately start one real manual run. It remains limited to at most five upload attempts and automation stays OFF. Only after a successful run can automation be enabled here.");
    q("#firstRealPasswordLabel").textContent=copy("Aktuelles Dashboard-Passwort","Current dashboard password");
    q("#firstRealPassword").placeholder=copy("Dashboard-Passwort","Dashboard password");
    q("#firstRealStart").textContent=copy("Begrenzten echten Lauf starten","Start limited real run");
    q("#firstRealEnable").textContent=copy("Automatik aktivieren","Enable automation");
    const intro=document.querySelector('section[data-page="6"] > p');
    if(intro)intro.innerHTML=copy("Der Assistent richtet alles ein und führt danach automatisch einen <strong>reinen Testlauf ohne Upload</strong> durch. Anschließend bleibt die Automatik AUS. Du kannst danach bewusst einen begrenzten echten Lauf bestätigen und erst nach dessen Erfolg die Automatik aktivieren.","The assistant provisions everything and then automatically performs a <strong>no-upload safety test</strong>. Automation then remains OFF. You can deliberately confirm one limited real run and enable automation only after that succeeds.");
  }
  const previousSetLang=setLang;
  setLang=function(l){previousSetLang(l);renderFirstRealCopy();};
  const previousSetInstalledMode=setInstalledMode;
  setInstalledMode=function(on){
    previousSetInstalledMode(on);
    maintenanceMode=Boolean(on);
    (maintenanceMode?maintenanceSlot:done).appendChild(panel);
    panel.classList.toggle("maintenance-panel",maintenanceMode);
    renderFirstRealCopy();
    setTimeout(pollFirstReal,0);
  };
  renderFirstRealCopy();

  async function pollFirstReal(){
    try{
      const d=await api("/api/install/first-real/status");
      const s=d.state||{};
      if(s.status==="running"){
        status(q("#firstRealStatus"),copy("Begrenzter echter Lauf läuft …","Limited real run is in progress …"),"warn");
        q("#firstRealStart").disabled=true;
        q("#firstRealEnable").disabled=true;
        setTimeout(pollFirstReal,1600);
        return;
      }
      if(s.status==="completed"){
        const st=s.result?.stats||{};
        status(q("#firstRealStatus"),copy((st.uploaded||0)+" hochgeladen · "+(st.unchanged||0)+" unverändert · "+(st.deferred||0)+" später · 0 fehlgeschlagen",(st.uploaded||0)+" uploaded · "+(st.unchanged||0)+" unchanged · "+(st.deferred||0)+" deferred · 0 failed"),"ok");
        q("#firstRealStart").disabled=true;
        q("#firstRealEnable").disabled=false;
        return;
      }
      if(s.status==="enabled"){
        status(q("#firstRealStatus"),copy("Automatik ist aktiviert. Der eingestellte Wochenplan gilt jetzt.","Automation is enabled. The configured weekly schedule is now active."),"ok");
        q("#firstRealStart").disabled=true;
        q("#firstRealEnable").disabled=true;
        return;
      }
      if(s.status==="failed"){
        status(q("#firstRealStatus"),s.error||copy("Der echte Lauf ist fehlgeschlagen. Automatik bleibt AUS und Testmodus bleibt aktiv.","The real run failed. Automation remains OFF and dry-run mode remains active."),"error");
        q("#firstRealStart").disabled=false;
        q("#firstRealEnable").disabled=!maintenanceMode;
        return;
      }
      status(q("#firstRealStatus"),maintenanceMode
        ? copy("Noch kein lokaler Freigabestatus gefunden. Falls das Dashboard den echten Lauf bereits bestätigt, gib das Passwort ein und klicke auf „Automatik aktivieren“.","No local activation state was found. If the dashboard already shows the real run as verified, enter the password and click “Enable automation”.")
        : copy("Bereit. Es wird erst nach deiner Bestätigung wirklich hochgeladen.","Ready. Nothing is uploaded until you confirm."));
      q("#firstRealStart").disabled=false;
      q("#firstRealEnable").disabled=!maintenanceMode;
    }catch(e){status(q("#firstRealStatus"),e.message,"error");q("#firstRealStart").disabled=false;}
  }

  q("#firstRealStart").addEventListener("click",async()=>{
    const password=q("#firstRealPassword").value;
    if(password.length<16){status(q("#firstRealStatus"),copy("Gib das aktuelle Dashboard-Passwort ein.","Enter the current dashboard password."),"error");return;}
    if(!confirm(copy("Jetzt einen echten manuellen Lauf starten? Dabei können höchstens fünf Dateien hochgeladen werden. Die Automatik bleibt AUS.","Start one real manual run now? At most five files can be uploaded. Automation remains OFF.")))return;
    const b=q("#firstRealStart");busy(b,true);q("#firstRealEnable").disabled=true;
    try{
      await api("/api/install/first-real/start","POST",{password});
      q("#firstRealPassword").value="";
      status(q("#firstRealStatus"),copy("Begrenzter echter Lauf wurde gestartet …","Limited real run started …"),"warn");
      pollFirstReal();
    }catch(e){status(q("#firstRealStatus"),e.message,"error");busy(b,false);}
  });

  q("#firstRealEnable").addEventListener("click",async()=>{
    const password=q("#firstRealPassword").value;
    if(password.length<16){status(q("#firstRealStatus"),copy("Gib das Dashboard-Passwort erneut ein, um die Automatik zu aktivieren.","Enter the dashboard password again to enable automation."),"error");return;}
    if(!confirm(copy("Automatische Synchronisation jetzt nach deinem eingestellten Wochenplan aktivieren?","Enable automatic synchronization now using your configured weekly schedule?")))return;
    const b=q("#firstRealEnable");busy(b,true);
    try{
      await api("/api/install/enable-automatic","POST",{password});
      q("#firstRealPassword").value="";
      await pollFirstReal();
    }catch(e){status(q("#firstRealStatus"),e.message,"error");busy(b,false);}
  });
  pollFirstReal();
})();
`

func (a *App) firstRealStatePath() string {
	return filepath.Join(a.workDir, "first-real-sync.json")
}

func firstRealPasswordValid(password string) bool {
	return len(password) >= 16 && len(password) <= 256 && !strings.ContainsAny(password, "\x00\r\n")
}

func (a *App) loadFirstRealState(meta *installMeta) firstRealSyncState {
	state := firstRealSyncState{Status: "idle"}
	raw, err := os.ReadFile(a.firstRealStatePath())
	if err != nil {
		return state
	}
	if json.Unmarshal(raw, &state) != nil {
		return firstRealSyncState{Status: "failed", Error: a.text("Lokaler Status des ersten echten Laufs ist beschädigt. Der Lauf muss sicher wiederholt werden.", "Local first-real-run state is corrupted. The run must be repeated safely.")}
	}
	if meta == nil || state.AppName != meta.AppName {
		return firstRealSyncState{Status: "idle"}
	}
	if state.Status == "running" && (state.StartedAt.IsZero() || state.StartedAt.Before(firstRealProcessStartedAt) || time.Since(state.StartedAt) > firstRealRunTimeout+2*time.Minute) {
		state.Status = "failed"
		state.Error = a.text("Der vorherige echte Lauf wurde nicht sauber abgeschlossen. Automatik bleibt AUS; starte den begrenzten Lauf erneut.", "The previous real run did not finish cleanly. Automation remains OFF; start the limited run again.")
		state.UpdatedAt = time.Now().UTC()
		_ = a.saveFirstRealState(state)
	}
	if state.Status == "completed" && !firstRealResultVerified(state.Result) {
		state.Status = "failed"
		state.Error = a.text("Der gespeicherte echte Lauf enthält keinen gültigen erfolgreichen Upload. Automatik bleibt AUS; starte den begrenzten Lauf erneut.", "The saved real run does not contain a valid successful upload. Automation remains OFF; retry the limited run.")
		state.UpdatedAt = time.Now().UTC()
		_ = a.saveFirstRealState(state)
	}
	return state
}

func (a *App) saveFirstRealState(state firstRealSyncState) error {
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return atomicWritePrivate(a.firstRealStatePath(), raw)
}

func cloneDashboardSettings(settings map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(settings)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func prepareFirstRealSettings(current map[string]any) (map[string]any, map[string]any, error) {
	if current == nil {
		return nil, nil, fmt.Errorf("Dashboard-Einstellungen fehlen")
	}
	enabled, ok := current["enabled"].(bool)
	if !ok || enabled {
		return nil, nil, fmt.Errorf("Sicherheitsprüfung: Automatik muss vor dem ersten echten Lauf AUS sein")
	}
	syncCurrent, ok := current["sync"].(map[string]any)
	if !ok {
		return nil, nil, fmt.Errorf("Dashboard-Synchronisationseinstellungen fehlen")
	}
	dryRun, ok := syncCurrent["dry_run"].(bool)
	if !ok || !dryRun {
		return nil, nil, fmt.Errorf("Sicherheitsprüfung: Testmodus muss vor dem ersten echten Lauf aktiv sein")
	}

	realSettings, err := cloneDashboardSettings(current)
	if err != nil {
		return nil, nil, err
	}
	restoreSettings, err := cloneDashboardSettings(current)
	if err != nil {
		return nil, nil, err
	}
	realSettings["enabled"] = false
	realSync, ok := realSettings["sync"].(map[string]any)
	if !ok {
		return nil, nil, fmt.Errorf("Dashboard-Synchronisationseinstellungen fehlen")
	}
	realSync["dry_run"] = false
	restoreSettings["enabled"] = false
	restoreSync, ok := restoreSettings["sync"].(map[string]any)
	if !ok {
		return nil, nil, fmt.Errorf("Dashboard-Synchronisationseinstellungen fehlen")
	}
	restoreSync["dry_run"] = true
	return realSettings, restoreSettings, nil
}

func recoverFirstRealSafeStartingSettings(current map[string]any) (map[string]any, bool, error) {
	if current == nil {
		return nil, false, fmt.Errorf("Dashboard-Einstellungen fehlen")
	}
	enabled, ok := current["enabled"].(bool)
	if !ok || enabled {
		return nil, false, fmt.Errorf("Sicherheitsprüfung: Automatik muss vor dem ersten echten Lauf AUS sein")
	}
	syncCurrent, ok := current["sync"].(map[string]any)
	if !ok {
		return nil, false, fmt.Errorf("Dashboard-Synchronisationseinstellungen fehlen")
	}
	dryRun, ok := syncCurrent["dry_run"].(bool)
	if !ok {
		return nil, false, fmt.Errorf("Dashboard-Testmodus fehlt")
	}
	if dryRun {
		return current, false, nil
	}
	safeSettings, err := cloneDashboardSettings(current)
	if err != nil {
		return nil, false, err
	}
	safeSettings["enabled"] = false
	safeSync, ok := safeSettings["sync"].(map[string]any)
	if !ok {
		return nil, false, fmt.Errorf("Dashboard-Synchronisationseinstellungen fehlen")
	}
	safeSync["dry_run"] = true
	return safeSettings, true, nil
}

func firstRealResultVerified(result *verificationResult) bool {
	if result == nil || result.Stats.Uploaded <= 0 || result.Stats.Failed != 0 {
		return false
	}
	return result.Outcome == "success" || result.Outcome == "partial"
}

func firstRealActivationVerified(state firstRealSyncState, dashboard dashboardState) bool {
	return dashboard.Activation.Enabled ||
		(state.Status == "completed" && firstRealResultVerified(state.Result)) ||
		dashboard.Activation.RealRunVerified
}

func diagnosticCodeFromMessage(message string) string {
	const marker = "Diagnosecode "
	start := strings.Index(message, marker)
	if start < 0 {
		return ""
	}
	start += len(marker)
	end := start
	for end < len(message) && end-start < 48 {
		char := message[end]
		if (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '-' {
			break
		}
		end++
	}
	if end == start {
		return ""
	}
	return message[start:end]
}

func (a *App) firstRealFailureMessage(reason string, stats verificationStats, reportMessage ...string) string {
	switch reason {
	case "remarkable_auth":
		return a.text("Die reMarkable-Cloud-Autorisierung ist abgelaufen oder wurde abgelehnt. Verbinde reMarkable unter den Zugangsdaten erneut und wiederhole den begrenzten Lauf.", "The reMarkable Cloud authorization expired or was rejected. Reconnect reMarkable under credentials and retry the limited run.")
	case "remarkable_target":
		return a.text("Der ausgewählte reMarkable-Zielordner ist nicht mehr verfügbar. Prüfe die Ordnerzuordnung und wiederhole den begrenzten Lauf.", "The selected reMarkable destination is no longer available. Check the folder mapping and retry the limited run.")
	case "remarkable_network":
		return a.text("Die Verbindung zur reMarkable Cloud wurde unterbrochen. Warte kurz und wiederhole den begrenzten Lauf.", "The connection to reMarkable Cloud was interrupted. Wait briefly and retry the limited run.")
	case "remarkable_timeout":
		return a.text("Das Lesen der reMarkable Cloud hat die Zeitgrenze erreicht. Prüfe die Verbindung und wiederhole den begrenzten Lauf. Erneutes Anmelden ist dafür nicht erforderlich.", "The reMarkable Cloud read reached its time limit. Check the connection and retry the limited run. Signing in again is not required for this timeout.")
	case "remarkable_document":
		return a.text("reMarkable hat ein Dokument nicht akzeptiert. Prüfe, ob die Quelldateien gültige PDF- oder EPUB-Dateien sind.", "reMarkable rejected a document. Check that the source files are valid PDF or EPUB documents.")
	case "remarkable_conflict":
		return a.text("Ein gleichnamiges reMarkable-Dokument konnte nicht sicher als versionierte Kopie übernommen werden. Das vorhandene Dokument blieb unverändert.", "A same-named reMarkable document could not be safely imported as a versioned copy. The existing document was left unchanged.")
	case "remarkable_generation":
		return a.text("Die reMarkable Cloud wurde parallel geändert. Der sichere Wiederholungsversuch konnte nicht abgeschlossen werden; warte, bis das Tablet vollständig synchronisiert ist, und wiederhole den begrenzten Lauf.", "reMarkable Cloud changed concurrently. The safe retry could not finish; wait until the tablet has fully synchronized, then retry the limited run.")
	case "remarkable_protocol":
		return a.text("Die reMarkable Cloud verwendet ein geändertes Sync-Protokoll. Aktualisiere die Cloud-Komponenten im Installer; erneutes Verbinden behebt diesen Fehler nicht.", "reMarkable Cloud is using a changed sync protocol. Update the cloud components in the installer; reconnecting does not fix this error.")
	case "remarkable_provider":
		message := a.text("Die reMarkable Cloud hat den Upload unerwartet abgelehnt. Erstelle im Installer einen Diagnosebericht; erneutes Verbinden ist ohne Autorisierungsfehler nicht erforderlich.", "reMarkable Cloud unexpectedly rejected the upload. Create a diagnostic report in the installer; reconnecting is unnecessary unless authorization failed.")
		if len(reportMessage) > 0 {
			if code := diagnosticCodeFromMessage(reportMessage[0]); code != "" {
				message += " " + a.text("Diagnosecode: ", "Diagnostic code: ") + code + "."
			}
		}
		return message
	case "iserv_auth":
		return a.text("IServ hat den WebDAV-Zugriff abgelehnt. Prüfe oder erneuere die IServ-Zugangsdaten im Installer.", "IServ rejected WebDAV access. Check or renew the IServ credentials in the installer.")
	case "iserv_source":
		return a.text("Ein ausgewählter IServ-Quellordner ist nicht mehr erreichbar. Prüfe die Ordnerzuordnung und deine Zugriffsrechte.", "A selected IServ source folder is no longer reachable. Check the folder mapping and your access permissions.")
	case "iserv_certificate":
		return a.text("Die sichere Verbindung zu IServ konnte nicht verifiziert werden. Schalte die Zertifikatsprüfung nicht aus; prüfe die WebDAV-Adresse oder kontaktiere die Schule.", "The secure connection to IServ could not be verified. Do not disable certificate verification; check the WebDAV address or contact the school.")
	case "iserv_storage":
		return a.text("Der geschützte Fly-Speicher ist voll oder nicht beschreibbar. Führe ein Cloud-Update aus und erstelle bei erneutem Auftreten einen Diagnosebericht.", "The protected Fly storage is full or not writable. Run a cloud update and create a diagnostic report if this happens again.")
	case "iserv_network":
		return a.text("Die Verbindung zu IServ wurde vorübergehend unterbrochen. Der Lauf wurde nach mehreren sicheren Versuchen angehalten; starte ihn später erneut.", "The connection to IServ was temporarily interrupted. The run stopped after several safe attempts; retry it later.")
	case "iserv_provider":
		return a.text("IServ WebDAV hat die Dateiübertragung unerwartet abgelehnt. Teste die IServ-Verbindung im Installer und wiederhole den begrenzten Lauf.", "IServ WebDAV unexpectedly rejected the file transfer. Test the IServ connection in the installer and retry the limited run.")
	case "upload_limit":
		if stats.Failed > 0 {
			return a.text("Mindestens ein Upload ist fehlgeschlagen; weitere Dateien wurden wegen des Sicherheitslimits zurückgestellt. Automatik bleibt AUS.", "At least one upload failed; remaining files were deferred by the safety limit. Automation remains OFF.")
		}
	}
	if a.text("de", "en") == "en" {
		return "The limited real run failed. Automation remains OFF."
	}
	return "Der begrenzte echte Lauf ist fehlgeschlagen. Automatik bleibt AUS."
}

func putDashboardSettings(base, password string, settings map[string]any) error {
	raw, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	body, status, err := adminRequest(base, password, http.MethodPut, "/api/admin/settings", raw)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("Dashboard-Einstellungen konnten nicht gespeichert werden (HTTP %d): %s", status, strings.TrimSpace(string(body)))
	}
	return nil
}

func cloudComponentsCurrent(meta *installMeta) bool {
	return meta != nil && (meta.Version == appVersion ||
		(meta.Version == "0.9.0-rc26" && currentCloudPayloadDigest == compatibleRC26PayloadDigest))
}

func (a *App) requireCurrentCloudComponents(w http.ResponseWriter, r *http.Request, meta *installMeta) bool {
	if cloudComponentsCurrent(meta) {
		return true
	}
	writeAPIError(w, r, http.StatusConflict,
		"Die Cloud-Komponenten sind nicht auf dem Stand dieses Installers. Öffne den Wartungsbereich, wähle „Cloud-Komponenten aktualisieren“ und wiederhole den Vorgang erst nach erfolgreichem Abschluss.",
		"The cloud components do not match this installer version. Open maintenance, choose “Update cloud components”, and retry only after it completes successfully.",
	)
	return false
}

func (a *App) handleFirstRealSyncStart(w http.ResponseWriter, r *http.Request) {
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	if !a.mappingSettingsIdle(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		Password string `json:"password"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil || !firstRealPasswordValid(in.Password) {
		writeAPIError(w, r, http.StatusBadRequest, "Gültiges Dashboard-Passwort erforderlich.", "A valid dashboard password is required.")
		return
	}
	meta := a.metaSnapshot()
	if meta == nil {
		writeAPIError(w, r, http.StatusConflict, "Installation ist noch nicht abgeschlossen.", "Installation is not complete yet.")
		return
	}
	if !a.requireCurrentCloudComponents(w, r, meta) {
		return
	}

	firstRealStartMu.Lock()
	state := a.loadFirstRealState(meta)
	if state.Status == "running" {
		firstRealStartMu.Unlock()
		writeAPIError(w, r, http.StatusConflict, "Der begrenzte echte Lauf läuft bereits.", "The limited real run is already running.")
		return
	}
	if state.Status == "completed" || state.Status == "enabled" {
		firstRealStartMu.Unlock()
		writeAPIError(w, r, http.StatusConflict, "Der begrenzte echte Lauf ist bereits erfolgreich abgeschlossen.", "The limited real run has already completed successfully.")
		return
	}
	state = firstRealSyncState{Status: "running", AppName: meta.AppName, StartedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := a.saveFirstRealState(state); err != nil {
		firstRealStartMu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": a.errorText(err)})
		return
	}
	firstRealStartMu.Unlock()

	password := in.Password
	go a.executeFirstRealSync(meta, password)
	writeJSON(w, http.StatusAccepted, map[string]bool{"started": true})
}

func (a *App) handleFirstRealSyncStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"state": a.loadFirstRealState(a.metaSnapshot())})
}

func (a *App) executeFirstRealSync(meta *installMeta, password string) {
	fail := func(err error) {
		state := firstRealSyncState{Status: "failed", AppName: meta.AppName, UpdatedAt: time.Now().UTC(), Error: a.errorText(err)}
		_ = a.saveFirstRealState(state)
		a.logText("Begrenzter echter Lauf fehlgeschlagen. Automatik bleibt AUS und Testmodus wird beibehalten.", "Limited real run failed. Automation remains OFF and dry-run mode is kept.")
	}

	before, err := getDashboardState(meta.DashboardURL, password)
	if err != nil {
		fail(fmt.Errorf("Dashboard-Anmeldung oder Verbindung fehlgeschlagen: %w", err))
		return
	}
	safeStartingSettings, recovered, err := recoverFirstRealSafeStartingSettings(before.Settings)
	if err != nil {
		fail(err)
		return
	}
	if recovered {
		a.logText("Ein unterbrochener echter Lauf wurde erkannt. Der sichere Testmodus wird vor dem neuen Versuch wiederhergestellt.", "An interrupted real run was detected. Restoring safe dry-run mode before retrying.")
		if err := putDashboardSettings(meta.DashboardURL, password, safeStartingSettings); err != nil {
			fail(fmt.Errorf("Sicherer Testmodus konnte vor dem erneuten Versuch nicht wiederhergestellt werden: %w", err))
			return
		}
		before.Settings = safeStartingSettings
	}
	realSettings, restoreSettings, err := prepareFirstRealSettings(before.Settings)
	if err != nil {
		fail(err)
		return
	}
	if err := putDashboardSettings(meta.DashboardURL, password, realSettings); err != nil {
		fail(err)
		return
	}

	previous := ""
	if before.Status.LastReport != nil {
		previous = before.Status.LastReport.FinishedAt
	}
	a.logText("Begrenzter echter Lauf wird bewusst gestartet. Automatik bleibt AUS; höchstens fünf Upload-Versuche sind erlaubt.", "Starting the deliberately limited real run. Automation remains OFF; at most five upload attempts are allowed.")
	started, runErr := a.startRunWithRetry(meta.DashboardURL, password, adminRequest, time.Sleep)

	var result *verificationResult
	if runErr == nil {
		deadline := time.Now().Add(firstRealRunTimeout)
		for time.Now().Before(deadline) {
			time.Sleep(3 * time.Second)
			dashboard, stateErr := getDashboardState(meta.DashboardURL, password)
			if stateErr != nil {
				continue
			}
			report := dashboard.Status.LastReport
			if !newerRunReport(report, previous) || !reportFollowsStart(report, started) {
				continue
			}
			result = report.result()
			switch {
			case report.DryRun:
				runErr = fmt.Errorf("Sicherheitsprüfung: der bestätigte echte Lauf war unerwartet weiterhin im Testmodus")
			case !report.OK || report.Outcome == "failed" || report.Error != "" || report.Stats == nil || result.Stats.Failed > 0:
				message := a.firstRealFailureMessage(report.Reason, result.Stats, report.Message)
				runErr = fmt.Errorf("%s%s", a.text("Echter Lauf fehlgeschlagen: ", "Limited real run failed: "), message)
			case !firstRealResultVerified(result):
				runErr = fmt.Errorf("Echter Lauf hat keine Datei erfolgreich hochgeladen. Automatik bleibt gesperrt; ändere oder ergänze eine passende Datei und starte den begrenzten Lauf erneut")
			}
			break
		}
		if result == nil && runErr == nil {
			runErr = fmt.Errorf("Echter Lauf hat innerhalb von 7 Minuten keinen Abschluss gemeldet")
		}
	}

	restoreErr := putDashboardSettings(meta.DashboardURL, password, restoreSettings)
	if restoreErr != nil {
		fail(fmt.Errorf("Testmodus konnte nach dem echten Lauf nicht sicher wiederhergestellt werden: %w", restoreErr))
		return
	}
	if runErr != nil {
		fail(runErr)
		return
	}

	state := firstRealSyncState{Status: "completed", AppName: meta.AppName, UpdatedAt: time.Now().UTC(), Result: result}
	if err := a.saveFirstRealState(state); err != nil {
		fail(err)
		return
	}
	a.logText("Begrenzter echter Lauf erfolgreich. Automatik bleibt AUS, bis sie ausdrücklich aktiviert wird.", "Limited real run succeeded. Automation remains OFF until explicitly enabled.")
}

func enableAutomaticWithRollback(put func(map[string]any) error, enabledSettings, rollbackSettings map[string]any, saveEnabled func() error) error {
	if err := put(enabledSettings); err != nil {
		return err
	}
	if err := saveEnabled(); err != nil {
		if rollbackErr := put(rollbackSettings); rollbackErr != nil {
			return fmt.Errorf("Aktivierung konnte lokal nicht gespeichert werden (%v); auch das sichere Zurücksetzen ist fehlgeschlagen: %w", err, rollbackErr)
		}
		return fmt.Errorf("Aktivierung konnte lokal nicht gespeichert werden und wurde deshalb sicher zurückgenommen: %w", err)
	}
	return nil
}

func (a *App) handleEnableAutomaticSync(w http.ResponseWriter, r *http.Request) {
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	if !a.mappingSettingsIdle(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		Password string `json:"password"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil || !firstRealPasswordValid(in.Password) {
		writeAPIError(w, r, http.StatusBadRequest, "Gültiges Dashboard-Passwort erforderlich.", "A valid dashboard password is required.")
		return
	}
	meta := a.metaSnapshot()
	if meta == nil {
		writeAPIError(w, r, http.StatusConflict, "Installation ist nicht verbunden.", "Installation is not connected.")
		return
	}
	if !a.requireCurrentCloudComponents(w, r, meta) {
		return
	}
	state := a.loadFirstRealState(meta)
	current, err := getDashboardState(meta.DashboardURL, in.Password)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": a.errorText(err)})
		return
	}
	if !firstRealActivationVerified(state, current) {
		writeAPIError(w, r, http.StatusConflict, "Automatik bleibt gesperrt, bis der begrenzte echte Lauf mindestens eine Datei erfolgreich hochgeladen hat.", "Automation remains locked until the limited real run has successfully uploaded at least one file.")
		return
	}
	if current.Activation.Enabled {
		state.Status = "enabled"
		state.AppName = meta.AppName
		state.UpdatedAt = time.Now().UTC()
		state.Error = ""
		if err := a.saveFirstRealState(state); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": a.errorText(err)})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	_, rollbackSettings, err := prepareFirstRealSettings(current.Settings)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": a.errorText(err)})
		return
	}
	enabledSettings, err := cloneDashboardSettings(rollbackSettings)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": a.errorText(err)})
		return
	}
	enabledSettings["enabled"] = true
	syncSettings, ok := enabledSettings["sync"].(map[string]any)
	if !ok {
		writeAPIError(w, r, http.StatusConflict, "Dashboard-Synchronisationseinstellungen fehlen.", "Dashboard sync settings are missing.")
		return
	}
	syncSettings["dry_run"] = false
	if err := enableAutomaticWithRollback(
		func(settings map[string]any) error {
			return putDashboardSettings(meta.DashboardURL, in.Password, settings)
		},
		enabledSettings,
		rollbackSettings,
		func() error {
			state.Status = "enabled"
			state.UpdatedAt = time.Now().UTC()
			state.Error = ""
			return a.saveFirstRealState(state)
		},
	); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": a.errorText(err)})
		return
	}
	a.logText("Automatische Synchronisation wurde nach erfolgreicher echter Verifikation bewusst aktiviert.", "Automatic synchronization was deliberately enabled after successful real verification.")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

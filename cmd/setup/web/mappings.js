(() => {
  for (const [de, en] of [
    ["Ordnerverknüpfungen bearbeiten", "Edit folder mappings"],
    ["Lade deine gespeicherten Zuordnungen und wähle links IServ, rechts reMarkable. Speichern startet keine Synchronisation.", "Load your saved mappings and choose IServ on the left, reMarkable on the right. Saving does not start a sync."],
    ["Gespeicherte Zuordnungen laden", "Load saved mappings"],
    ["Noch keine Zuordnungen geladen.", "No mappings loaded yet."],
    ["Ordnerzugang für diese Sitzung verbinden", "Connect folder access for this session"],
    ["Zum Durchsuchen brauchst du die IServ-Verbindung und einen reMarkable-Einmalcode. Diese Anmeldung gilt nur lokal für die aktuelle Sitzung. Die Cloud-Zugangsdaten bleiben unverändert.", "Browsing requires an IServ connection and a reMarkable one-time code. This sign-in is local to this session. Cloud credentials stay unchanged."],
    ["Zuordnungen speichern", "Save mappings"], ["Schließen", "Close"],
  ]) staticEN.set(de, en);
  translateStatic();
  const panel = $("#maintenanceMappingEditor");
  const message = $("#maintenanceMappingsStatus");
  const password = $("#maintenanceMappingsPassword");
  const load = $("#maintenanceMappingsLoad");
  const save = $("#maintenanceMappingsSave");
  const cancel = $("#maintenanceMappingsCancel");
  const service = document.querySelector('[data-page="3"]');
  const workspace = $("#linkSection");
  const serviceAnchor = document.createComment("wizard services");
  const workspaceAnchor = document.createComment("wizard mappings");
  service.before(serviceAnchor);
  workspace.before(workspaceAnchor);
  const copy = (de, en) => LANG === "en" ? en : de;
  let installed = false;
  let opened = false;
  let saving = false;
  let saved = "[]";
  let revision = "";
  const serialize = () => JSON.stringify(sources);
  const dirty = () => opened && serialize() !== saved;
  function updateButtons() {
    save.disabled = saving || !dirty() || sources.length < 1;
    load.disabled = saving;
    cancel.disabled = saving;
    document.querySelectorAll("#mappings .mn").forEach(n => n.maxLength = 60);
  }
  function mount(on) {
    installed = on;
    if (on) {
      $("#mappingServiceSlot").append(service);
      $("#mappingWorkspaceSlot").append(workspace);
      [service, workspace].forEach(n => n.classList.remove("hidden", "step-hidden"));
      service.querySelector("h2").textContent = copy("Ordnerzugang verbinden", "Connect folder access");
      workspace.querySelector("h2").textContent = copy("Ordner verknüpfen", "Link folders");
      if (!davConnected) status($("#davStatus"), copy("Verbinde IServ unter „Ordnerzugang für diese Sitzung verbinden“.", "Connect IServ under “Connect folder access for this session”."));
      if (!rmConnected) status($("#rmBrowserStatus"), copy("Verbinde reMarkable unter „Ordnerzugang für diese Sitzung verbinden“.", "Connect reMarkable under “Connect folder access for this session”."));
    } else {
      serviceAnchor.after(service);
      workspaceAnchor.after(workspace);
      service.querySelector("h2").textContent = copy("3. IServ und reMarkable verbinden", "3. Connect IServ and reMarkable");
      workspace.querySelector("h2").textContent = copy("4. Ordner verknüpfen", "4. Link folders");
      opened = false;
      panel.classList.add("hidden");
    }
  }
  const oldInstalledMode = setInstalledMode;
  setInstalledMode = on => { oldInstalledMode(on); mount(on); };
  const oldSetLang = setLang;
  setLang = language => {
    oldSetLang(language);
    if (installed) mount(true);
    if (opened) { renderMappings(); updateButtons(); }
  };
  // A very fast state response may have arrived before this script was parsed.
  if (!$("#maintenanceCard").classList.contains("hidden")) mount(true);

  load.addEventListener("click", async () => {
    if (dirty() && !confirm(copy("Ungespeicherte Zuordnungen verwerfen und den Cloud-Stand neu laden?", "Discard unsaved mappings and reload the cloud state?"))) return;
    busy(load, true);
    try {
      if (!password.value) throw new Error(copy("Gib das aktuelle Dashboard-Passwort ein.", "Enter the current dashboard password."));
      const result = await api("/api/maintenance/mappings/read", "POST", {password: password.value});
      sources = result.sources.map(s => ({name:s.name, iserv_path:s.iserv_path, remarkable_dir:s.remarkable_dir}));
      saved = serialize();
      revision = result.revision;
      opened = true;
      mount(true);
      panel.classList.remove("hidden");
      $("#mappingConnections").open = !davConnected || !rmConnected;
      renderMappings();
      clearSelected();
      status(message, copy("Gespeicherte Zuordnungen geladen. Änderungen werden erst mit „Zuordnungen speichern“ übernommen.", "Saved mappings loaded. Changes take effect only after Save mappings."), "ok");
    } catch (error) { status(message, error.message, "error"); }
    finally { busy(load, false); updateButtons(); }
  });

  function finishSave(ok, text, nextRevision, draft) {
    saving = false;
    panel.querySelectorAll("button, input").forEach(n => n.disabled = false);
    setRMLoading(rmLoading);
    if (ok) { saved = draft; revision = nextRevision; }
    status(message, text, ok ? "ok" : "error");
    updateButtons();
  }
  async function waitForSave(nextRevision, draft) {
    try {
      const state = await api("/api/state");
      if (state.maintenance_status === "running") { setTimeout(() => waitForSave(nextRevision, draft), 1500); return; }
      if (state.maintenance_action !== "mappings" || state.maintenance_status !== "completed") {
        finishSave(false, state.maintenance_error || copy("Speichern konnte nicht bestätigt werden. Lade den Cloud-Stand erneut.", "Saving could not be confirmed. Reload the cloud state."));
        return;
      }
      finishSave(true, copy("Zuordnungen gespeichert. Es wurde kein Lauf gestartet. Prüfe die neuen Zuordnungen vor der Automatik mit dem sicheren Testlauf.", "Mappings saved. No run was started. Check the new mappings with the safe test before enabling automation."), nextRevision, draft);
    } catch (error) {
      finishSave(false, copy("Die Verbindung wurde unterbrochen. Lade die Zuordnungen erneut, bevor du weiterarbeitest.", "The connection was interrupted. Reload the mappings before continuing."));
    }
  }
  save.addEventListener("click", async () => {
    if (save.disabled) return;
    const draft = serialize();
    saving = true;
    panel.querySelectorAll("button, input").forEach(n => n.disabled = true);
    updateButtons();
    status(message, copy("Zuordnungen werden gespeichert …", "Saving mappings …"));
    try {
      const result = await api("/api/maintenance/mappings/save", "POST", {password:password.value, revision, sources:JSON.parse(draft)});
      await waitForSave(result.revision, draft);
    } catch (error) { finishSave(false, error.message); }
  });
  cancel.addEventListener("click", () => {
    if (dirty() && !confirm(copy("Ungespeicherte Änderungen verwerfen?", "Discard unsaved changes?"))) return;
    sources = JSON.parse(saved);
    renderMappings();
    opened = false;
    panel.classList.add("hidden");
    password.value = "";
    status(message, copy("Ordnerauswahl geschlossen.", "Folder selection closed."));
  });
  panel.addEventListener("input", updateButtons);
  panel.addEventListener("click", () => queueMicrotask(updateButtons));
  window.addEventListener("beforeunload", event => { if (dirty()) { event.preventDefault(); event.returnValue = ""; } });
})();

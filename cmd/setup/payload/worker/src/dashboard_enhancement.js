export const DASHBOARD_ENHANCEMENT_JS = String.raw`

// Product-polish layer: activation safety, unsaved-change feedback and
// keyboard-complete weekday navigation. It intentionally builds on the small
// dependency-free dashboard above instead of adding a framework.
(function () {
  let dirty = false;
  let activation = null;
  const actions = document.querySelector("#actions");
  const message = document.querySelector("#message");
  const modePill = document.querySelector("#modePill");
  const activationNote = document.querySelector("#activationNote");
  const enabledControl = document.querySelector("#enabled");
  const dryRunControl = document.querySelector("#dryRun");
  const runButton = document.querySelector("#run");

  function copy(de, en) {
    return LANG === "en" ? en : de;
  }

  function markDirty() {
    dirty = true;
    actions?.classList.add("dirty");
    if (message && !message.classList.contains("error")) {
      message.textContent = copy("Ungespeicherte Änderungen.", "Unsaved changes.");
      message.className = "message dirty";
    }
  }

  function clearDirty() {
    dirty = false;
    actions?.classList.remove("dirty");
  }

  function applyActivation(next) {
    activation = next && typeof next === "object" ? next : { real_run_verified: false, enabled: false };
    const unlocked = activation.enabled === true;
    const verified = activation.real_run_verified === true;

    if (enabledControl) enabledControl.disabled = !unlocked;
    if (dryRunControl) dryRunControl.disabled = !unlocked;

    if (modePill) {
      modePill.classList.toggle("ok", unlocked);
      modePill.classList.toggle("warn", !unlocked);
      modePill.textContent = unlocked
        ? copy("Automatik freigegeben", "Automation unlocked")
        : verified
          ? copy("Echter Lauf bestätigt", "Real run verified")
          : copy("Sicherer Startmodus", "Safe setup mode");
    }

    if (activationNote) {
      activationNote.innerHTML = '<span class="safety-dot" aria-hidden="true"></span><span></span>';
      const text = activationNote.querySelector("span:last-child");
      if (text) {
        text.textContent = unlocked
          ? copy("Die sichere Ersteinrichtung ist abgeschlossen. Automatik und Testmodus können jetzt bewusst verwaltet werden.", "Safe initial setup is complete. Automation and test mode can now be managed deliberately.")
          : verified
            ? copy("Der begrenzte echte Lauf war erfolgreich. Aktiviere die Automatik im Setup-Assistenten, wenn der Wochenplan starten soll.", "The limited real run succeeded. Enable automation in the setup companion when the weekly schedule should start.")
            : copy("Echte Uploads und Automatik bleiben hier gesperrt, bis der begrenzte echte Lauf im Setup-Assistenten erfolgreich war.", "Real uploads and automation stay locked here until the limited real run succeeds in the setup companion.");
      }
    }

    if (runButton && !unlocked) {
      runButton.textContent = copy("Speichern & sicheren Test starten", "Save & run safe test");
      runButton.title = copy("Während der Ersteinrichtung ist hier nur Testmodus erlaubt.", "Only test mode is allowed here during initial setup.");
    } else if (runButton) {
      runButton.textContent = copy("Speichern & jetzt synchronisieren", "Save & sync now");
      runButton.removeAttribute("title");
    }
  }

  function applySafeFailureMessage(status) {
    const reason = status?.last_report?.reason;
    if (!String(reason || "").startsWith("iserv_")) return;
    const messages = {
      iserv_auth: copy("IServ hat den WebDAV-Zugriff abgelehnt. Prüfe oder erneuere die IServ-Zugangsdaten im Installer.", "IServ rejected WebDAV access. Check or renew the IServ credentials in the installer."),
      iserv_source: copy("Ein ausgewählter IServ-Quellordner ist nicht mehr erreichbar. Prüfe die Ordnerzuordnung und deine Zugriffsrechte.", "A selected IServ source folder is no longer reachable. Check the folder mapping and your access permissions."),
      iserv_certificate: copy("Die sichere Verbindung zu IServ konnte nicht verifiziert werden. Schalte die Zertifikatsprüfung nicht aus; prüfe die WebDAV-Adresse oder kontaktiere die Schule.", "The secure connection to IServ could not be verified. Do not disable certificate verification; check the WebDAV address or contact the school."),
      iserv_storage: copy("Der geschützte Fly-Speicher ist voll oder nicht beschreibbar. Führe im Installer ein Cloud-Update aus und erstelle bei erneutem Auftreten einen Diagnosebericht.", "The protected Fly storage is full or not writable. Run a cloud update in the installer and create a diagnostic report if this happens again."),
      iserv_network: copy("Die Verbindung zu IServ wurde vorübergehend unterbrochen. Der Lauf wurde nach mehreren sicheren Versuchen angehalten; starte ihn später erneut.", "The connection to IServ was temporarily interrupted. The run stopped after several safe attempts; retry it later."),
      iserv_provider: copy("IServ WebDAV hat die Dateiübertragung unerwartet abgelehnt. Teste die IServ-Verbindung im Installer und starte den Lauf erneut.", "IServ WebDAV unexpectedly rejected the file transfer. Test the IServ connection in the installer and retry the run."),
    };
    const detail = document.querySelector("#status .status-result > .muted");
    if (detail && messages[reason]) detail.textContent = messages[reason];
  }

  async function refreshActivation() {
    try {
      const body = await api("/api/admin/settings");
      applyActivation(body.activation);
      applySafeFailureMessage(body.status);
    } catch {
      // The normal dashboard error surface remains authoritative.
    }
  }

  const originalLoad = load;
  load = async function () {
    await originalLoad();
    await refreshActivation();
  };

  document.querySelector("main")?.addEventListener("input", (event) => {
    if (event.target instanceof HTMLInputElement || event.target instanceof HTMLSelectElement) markDirty();
  });
  document.querySelector("main")?.addEventListener("change", (event) => {
    if (event.target instanceof HTMLInputElement || event.target instanceof HTMLSelectElement) markDirty();
  });
  document.querySelector("main")?.addEventListener("click", (event) => {
    const button = event.target.closest("button");
    if (!button) return;
    if (button.id === "addWindow" || button.id === "addSource" || button.id === "applyCopy" || button.classList.contains("remove-window") || button.classList.contains("remove-source")) {
      setTimeout(markDirty, 0);
    }
  });

  if (message) {
    new MutationObserver(() => {
      const value = message.textContent || "";
      if (/^(Gespeichert|Aktuell|Gespeichert und gestartet|Saved|Up to date|Saved and started)/.test(value)) clearDirty();
    }).observe(message, { childList: true, characterData: true, subtree: true });
  }

  window.addEventListener("beforeunload", (event) => {
    if (!dirty) return;
    event.preventDefault();
    event.returnValue = "";
  });

  const tabs = document.querySelector("#dayTabs");
  function normalizeTabs() {
    if (!tabs) return;
    const buttons = Array.from(tabs.querySelectorAll(".day-tab"));
    buttons.forEach((button) => {
      const selected = button.classList.contains("active");
      button.setAttribute("aria-selected", selected ? "true" : "false");
      button.tabIndex = selected ? 0 : -1;
    });
  }
  if (tabs) {
    new MutationObserver(normalizeTabs).observe(tabs, { childList: true, subtree: true, attributes: true, attributeFilter: ["class"] });
    tabs.addEventListener("keydown", (event) => {
      const buttons = Array.from(tabs.querySelectorAll(".day-tab"));
      const currentIndex = buttons.indexOf(document.activeElement);
      if (currentIndex < 0 || buttons.length === 0) return;
      let nextIndex = currentIndex;
      if (event.key === "ArrowRight" || event.key === "ArrowDown") nextIndex = (currentIndex + 1) % buttons.length;
      else if (event.key === "ArrowLeft" || event.key === "ArrowUp") nextIndex = (currentIndex - 1 + buttons.length) % buttons.length;
      else if (event.key === "Home") nextIndex = 0;
      else if (event.key === "End") nextIndex = buttons.length - 1;
      else return;
      event.preventDefault();
      buttons[nextIndex].focus();
      buttons[nextIndex].click();
    });
  }

  document.querySelector("#refresh")?.addEventListener("click", () => setTimeout(refreshActivation, 0));
  document.querySelector("#langDE")?.addEventListener("click", () => setTimeout(() => applyActivation(activation), 0));
  document.querySelector("#langEN")?.addEventListener("click", () => setTimeout(() => applyActivation(activation), 0));

  normalizeTabs();
  setTimeout(refreshActivation, 0);
})();
`;

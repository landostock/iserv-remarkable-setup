export const DASHBOARD_HTML = String.raw`<!doctype html>
<html lang="de">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <meta name="color-scheme" content="light">
  <title>IServ → reMarkable</title>
  <style>
    :root{color-scheme:light;--bg:#f3f6f6;--card:#fff;--ink:#17212a;--muted:#66747f;--line:#dbe3e7;--accent:#176b5b;--accent-deep:#115649;--accent2:#e8f5f1;--danger:#9d2f2f;--danger-bg:#fff2f2;--ok:#176b40;--warn:#7a5900;--warn-bg:#fff8e6;--shadow:0 12px 36px rgba(27,45,54,.07);--radius:20px;--control:44px}
    *{box-sizing:border-box}html{scroll-behavior:smooth}body{margin:0;font:15px/1.5 system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;color:var(--ink);background:radial-gradient(circle at 10% 0%,rgba(23,107,91,.075),transparent 29rem),linear-gradient(180deg,#f8fafb,var(--bg));min-height:100vh}main{width:min(1120px,calc(100% - 30px));margin:24px auto 72px}
    h1,h2,h3,p{margin-top:0}h1{margin-bottom:5px;font-size:clamp(28px,4vw,43px);letter-spacing:0;line-height:1.04}h2{margin-bottom:12px;font-size:20px;letter-spacing:0}h3{margin-bottom:4px;font-size:19px}.muted{color:var(--muted)}.eyebrow{display:block;color:var(--muted);font-size:12px;font-weight:800;letter-spacing:0;text-transform:uppercase;margin-bottom:4px}
    header{display:flex;justify-content:space-between;align-items:center;gap:18px;padding:20px 22px;margin-bottom:18px;border:1px solid rgba(220,227,231,.95);border-radius:22px;background:rgba(255,255,255,.92);box-shadow:var(--shadow);backdrop-filter:blur(12px)}.brand-sub{margin:0;color:var(--muted);max-width:56ch}.header-actions{display:flex;gap:7px;flex-wrap:wrap;align-items:center;justify-content:flex-end}.lang-switch{display:flex;gap:3px;padding:3px;border:1px solid var(--line);border-radius:12px;background:#f4f7f8}.lang-switch button{min-width:44px;padding-inline:10px}
    .grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:16px}.card{min-width:0;background:var(--card);border:1px solid var(--line);border-radius:var(--radius);padding:20px;box-shadow:var(--shadow)}.wide{grid-column:1/-1}.card-head{display:flex;justify-content:space-between;gap:14px;align-items:flex-start;flex-wrap:wrap}.summary-pill{padding:7px 11px;border:1px solid rgba(23,107,91,.11);border-radius:999px;background:var(--accent2);color:var(--accent-deep);font-weight:760;font-size:13px;white-space:nowrap}.summary-pill.warn{background:var(--warn-bg);border-color:#ecdba7;color:var(--warn)}.summary-pill.ok{background:#edf8f3;border-color:#cfe9dc;color:var(--ok)}
    label{display:block;font-weight:680;margin:10px 0 5px}input,select,button{font:inherit}input[type=text],input[type=time],input[type=number],select{width:100%;min-width:0;min-height:var(--control);padding:10px 11px;border:1px solid #c4ced5;border-radius:11px;background:#fff;color:var(--ink);box-shadow:inset 0 1px 1px rgba(20,30,40,.02)}input:focus,select:focus,button:focus-visible{outline:3px solid rgba(23,107,91,.18);outline-offset:2px}input:focus,select:focus{border-color:var(--accent)}button{min-height:var(--control);border:0;border-radius:11px;padding:10px 14px;cursor:pointer;font-weight:760;transition:transform .14s ease,box-shadow .14s ease,background .14s ease}button.primary{background:var(--accent);color:#fff;box-shadow:0 7px 18px rgba(23,107,91,.17)}button.secondary{background:var(--accent2);color:var(--accent-deep)}button.ghost{background:#eef2f4;color:#34424e}button.remove{background:var(--danger-bg);color:var(--danger)}button:disabled{opacity:.52;cursor:not-allowed;transform:none!important}
    .toggle-stack{display:grid;gap:10px}.toggle-card{display:flex;gap:12px;align-items:flex-start;margin:0;padding:13px;border:1px solid #e2e8eb;border-radius:14px;background:#fbfcfd;cursor:pointer;transition:border-color .14s ease,background .14s ease}.toggle-card:has(input:checked){border-color:#bcdacf;background:#f2faf7}.toggle-card:has(input:disabled){cursor:not-allowed;opacity:.68}.toggle-card input{width:21px;height:21px;margin-top:1px;accent-color:var(--accent)}.toggle-title{display:block;font-weight:800}.toggle-help{display:block;color:var(--muted);font-size:13px;margin-top:2px}.note{border:1px solid #ead9a8;padding:11px 12px;background:var(--warn-bg);border-radius:11px;margin-top:12px}.safety-note{display:flex;gap:9px;align-items:flex-start}.safety-dot{width:9px;height:9px;margin-top:7px;border-radius:999px;background:var(--accent);box-shadow:0 0 0 4px var(--accent2);flex:0 0 auto}
    .status{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr);gap:10px}.status>div{min-width:0;background:#f7f9fa;border:1px solid #edf0f2;border-radius:12px;padding:13px;min-height:82px;overflow-wrap:anywhere}.status .status-result{grid-column:1/-1;min-height:112px;background:linear-gradient(180deg,#fbfdfc,#f6faf8)}.status small{display:block;color:var(--muted);margin-bottom:4px}.status strong{display:block;line-height:1.28}.status .status-result strong{font-size:19px;margin-bottom:5px}
    .schedule-head{display:grid;grid-template-columns:minmax(0,1fr) minmax(190px,250px);gap:18px;align-items:end}.schedule-head h2{margin-bottom:4px}.interval-field label{margin-top:0}.schedule-workspace{display:grid;grid-template-columns:250px 1fr;gap:16px;margin-top:18px;align-items:start}.day-sidebar,.day-editor{border:1px solid var(--line);border-radius:16px;background:#fbfcfd}.day-sidebar{padding:13px}.day-editor{padding:16px}.side-label{font-size:12px;font-weight:800;letter-spacing:0;text-transform:uppercase;color:var(--muted);margin:0 0 10px}.day-tabs{display:grid;gap:8px}.day-tab{display:flex;align-items:center;justify-content:space-between;gap:10px;width:100%;padding:10px 11px;border:1px solid var(--line);background:#fff;color:#34424e;text-align:left}.day-meta{display:flex;flex-direction:column;align-items:flex-start}.day-name{font-weight:800}.day-sub{font-size:12px;color:var(--muted)}.day-count{display:flex;min-width:29px;height:29px;align-items:center;justify-content:center;padding:0 8px;border-radius:999px;background:#eef2f4;font-size:12px}.day-tab.active{border-color:var(--accent);background:linear-gradient(135deg,var(--accent),#1d7d69);color:#fff;box-shadow:0 8px 20px rgba(23,107,91,.16)}.day-tab.active .day-sub{color:rgba(255,255,255,.82)}.day-tab.active .day-count{background:rgba(255,255,255,.18)}
    .day-editor-head{display:flex;justify-content:space-between;align-items:flex-start;gap:14px;margin-bottom:14px;flex-wrap:wrap}.editor-actions{display:flex;gap:8px;flex-wrap:wrap}.windows-stack{display:grid;gap:11px}.window-card{display:grid;grid-template-columns:auto 1fr auto;gap:13px;align-items:center;padding:13px;border:1px solid #e4e9ed;border-radius:14px;background:#fff}.window-number{min-width:88px;font-weight:800;color:#53616d}.time-pair{display:grid;grid-template-columns:1fr auto 1fr;gap:9px;align-items:end}.time-pair label{margin-top:0}.time-arrow{padding-bottom:11px;color:#82909a;font-weight:800}.empty-windows{padding:16px;border:1px dashed #cfd8df;border-radius:13px;background:#fff;color:var(--muted)}.copy-panel{margin-top:13px;padding:14px;border:1px solid #d6e7e1;border-radius:13px;background:#f7fcfa}.copy-panel[hidden]{display:none}.copy-days{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:8px;margin:10px 0 13px}.copy-days label{display:flex;align-items:center;gap:8px;margin:0;padding:8px 9px;border:1px solid #e1e7e9;border-radius:9px;background:#fff}.copy-days input{width:18px;height:18px;accent-color:var(--accent)}.copy-actions{display:flex;gap:8px;flex-wrap:wrap}
    .mapping-card{display:grid;grid-template-columns:.75fr 1.25fr 1.25fr auto;gap:9px;align-items:end;padding:13px;margin-top:10px;border:1px solid #e4e9ed;border-radius:14px;background:#fbfcfd}.mapping-card label{margin-top:0}.types{display:flex;gap:9px;flex-wrap:wrap}.types label{display:flex;gap:8px;align-items:center;margin:0;padding:11px 13px;border:1px solid var(--line);border-radius:12px;background:#fbfcfd}.types input{width:19px;height:19px;accent-color:var(--accent)}
    .actions{position:sticky;bottom:12px;z-index:10;display:flex;flex-wrap:wrap;gap:10px;align-items:center;margin-top:18px;padding:12px;border-radius:16px;background:rgba(255,255,255,.96);border:1px solid var(--line);box-shadow:0 16px 38px rgba(20,35,43,.13);backdrop-filter:blur(12px)}.actions.dirty{border-color:#d9c98f}.message{flex:1;min-width:190px;color:var(--muted)}.message.ok{color:var(--ok)}.message.error{color:var(--danger)}.message.dirty{color:var(--warn)}
    #langDE.active,#langEN.active{background:var(--accent);color:#fff}
    @media(hover:hover) and (pointer:fine){button:not(:disabled):hover{transform:translateY(-1px)}button.primary:not(:disabled):hover{box-shadow:0 9px 22px rgba(23,107,91,.23)}button.secondary:not(:disabled):hover{background:#dff0ea}button.ghost:not(:disabled):hover{background:#e5ebee}.day-tab:not(.active):hover{border-color:#b8d3ca;background:#f6fbf9}}
    @media(max-width:820px){.grid{grid-template-columns:1fr}.wide{grid-column:auto}.schedule-head{grid-template-columns:1fr}.schedule-workspace{grid-template-columns:1fr}.day-tabs{grid-template-columns:repeat(4,minmax(0,1fr))}.day-tab{min-width:0}.day-sub{display:none}.mapping-card{grid-template-columns:1fr 1fr}.mapping-card button{grid-column:1/-1}.window-card{grid-template-columns:1fr}.window-number{min-width:0}.copy-days{grid-template-columns:repeat(2,minmax(0,1fr))}}
    @media(max-width:540px){main{width:min(100% - 18px,1120px);margin-top:10px}header{display:block;border-radius:18px;padding:18px}.header-actions{justify-content:flex-start;margin-top:14px}.card{border-radius:18px;padding:17px}.status{grid-template-columns:1fr}.status .status-result{grid-column:auto}.day-tabs{grid-template-columns:repeat(2,minmax(0,1fr))}.time-pair{grid-template-columns:1fr 1fr}.time-arrow{display:none}.mapping-card{grid-template-columns:1fr}.copy-days{grid-template-columns:1fr}.actions{bottom:8px}.actions button{flex:1 1 100%}.message{order:-1;flex-basis:100%}}
    @media(prefers-reduced-motion:reduce){html{scroll-behavior:auto}*,*::before,*::after{animation-duration:.001ms!important;animation-iteration-count:1!important;transition-duration:.001ms!important;scroll-behavior:auto!important}}
    @media(forced-colors:active){.card,header,.actions,.day-sidebar,.day-editor,.window-card,.toggle-card{box-shadow:none}.summary-pill,.day-tab.active{forced-color-adjust:auto}.safety-dot{box-shadow:none}}
  </style>
</head>
<body>
<main>
  <header>
    <div><h1>IServ → reMarkable</h1><p class="brand-sub">Synchronisation, Zeitplan und Ordner zentral verwalten</p></div>
    <div class="header-actions"><div class="lang-switch" role="group" aria-label="Sprache"><button id="langDE" class="ghost" type="button" aria-label="Deutsch">DE</button><button id="langEN" class="ghost" type="button" aria-label="English">EN</button></div><button id="refresh" class="ghost" type="button">Status aktualisieren</button><button id="logout" class="ghost" type="button">Abmelden</button></div>
  </header>
  <div class="grid">
    <section class="card">
      <div class="card-head"><div><span class="eyebrow">Steuerung</span><h2>Betrieb</h2></div><span id="modePill" class="summary-pill">Sicherer Startmodus</span></div>
      <div class="toggle-stack">
        <label class="toggle-card"><input id="enabled" type="checkbox"><span><span class="toggle-title">Automatische Synchronisation</span><span class="toggle-help">Startet nur innerhalb des Wochenplans.</span></span></label>
        <label class="toggle-card"><input id="dryRun" type="checkbox"><span><span class="toggle-title">Testmodus</span><span class="toggle-help">Prüft alles, lädt aber keine Dateien hoch.</span></span></label>
      </div>
      <div id="activationNote" class="note safety-note"><span class="safety-dot" aria-hidden="true"></span><span>Während der sicheren Ersteinrichtung bleiben echte Uploads und Automatik gesperrt.</span></div>
    </section>

    <section class="card">
      <div class="card-head"><div><span class="eyebrow">Aktivität</span><h2>Letzter Lauf</h2></div></div>
      <div id="status" class="status" aria-live="polite"><div>Lade Status …</div></div>
    </section>

    <section class="card wide">
      <div class="schedule-head">
        <div><div class="card-head"><div><span class="eyebrow">Automatik</span><h2>Wochenplan</h2></div><span id="scheduleSummary" class="summary-pill">–</span></div><p class="muted">Jeder Wochentag kann eigene Zeitfenster haben. Ohne Zeitfenster bleibt der Tag inaktiv.</p></div>
        <div class="interval-field"><label for="interval">Innerhalb der Zeitfenster starten alle</label><select id="interval"><option value="5">5 Minuten</option><option value="10">10 Minuten</option><option value="15">15 Minuten</option><option value="20">20 Minuten</option><option value="30">30 Minuten</option><option value="60">60 Minuten</option></select></div>
      </div>
      <div class="schedule-workspace">
        <aside class="day-sidebar"><div class="side-label">Wochentage</div><div id="dayTabs" class="day-tabs" role="tablist" aria-label="Wochentag auswählen"></div></aside>
        <div class="day-editor">
          <div class="day-editor-head"><div><span class="eyebrow">Zeitfenster für</span><h3 id="dayTitle">Montag</h3><p id="dayHint" class="muted">–</p></div><div class="editor-actions"><button id="addWindow" class="secondary" type="button">+ Zeitfenster</button><button id="copyTimes" class="ghost" type="button">Zeiten auf andere Wochentage anwenden</button></div></div>
          <div id="windows" class="windows-stack"></div>
          <div id="emptyWindows" class="empty-windows" hidden>An diesem Tag ist keine automatische Synchronisation geplant.</div>
          <div id="copyPanel" class="copy-panel" hidden><strong>Diese Zeitfenster übernehmen für:</strong><div id="copyDays" class="copy-days"></div><div class="copy-actions"><button id="applyCopy" class="secondary" type="button">Auswahl übernehmen</button><button id="cancelCopy" class="ghost" type="button">Abbrechen</button></div></div>
        </div>
      </div>
    </section>

    <section class="card wide">
      <div class="card-head"><div><span class="eyebrow">Verknüpfungen</span><h2>IServ-Ordner → reMarkable-Ordner</h2><p class="muted">Hier kannst du vorhandene Zuordnungen später anpassen.</p></div><button id="addSource" class="secondary" type="button">+ Ordnerzuordnung</button></div>
      <div id="sources"></div>
    </section>

    <section class="card"><span class="eyebrow">Dateiauswahl</span><h2>Dateitypen</h2><div class="types"><label><input id="typePdf" type="checkbox"> PDF</label><label><input id="typeEpub" type="checkbox"> EPUB</label></div></section>
    <section class="card"><span class="eyebrow">Schutzgrenzen</span><h2>Dateifilter</h2><label for="lookback">Nur Dateien der letzten Tage</label><input id="lookback" type="number" min="1" max="3650"><label for="maxFile">Maximale Dateigröße in MB</label><input id="maxFile" type="number" min="1" max="95"></section>
  </div>
  <div id="actions" class="actions"><button id="save" class="primary" type="button">Änderungen speichern</button><button id="run" class="secondary" type="button">Speichern & jetzt synchronisieren</button><span id="message" class="message" aria-live="polite">Noch keine Änderungen gespeichert.</span></div>
</main>
<script src="/ui.js"></script>
</body>
</html>`;

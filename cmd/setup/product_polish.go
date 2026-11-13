package main

import (
	"net/http"
	"strings"
)

// polishedRootHandler keeps the setup HTML dependency-free while adding the
// final interaction/accessibility layer after the safety-flow wrapper has
// produced the page. The injected code stays inside the existing CSP-approved
// style/script elements.
func (a *App) polishedRootHandler() http.Handler {
	base := a.rootHandler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			if enhanced, ok := injectProductPolish(string(body)); ok {
				body = []byte(enhanced)
				w.Header().Del("Content-Length")
			}
		}
		w.WriteHeader(statusCode)
		_, _ = w.Write(body)
	})
}

func injectProductPolish(page string) (string, bool) {
	styleIndex := strings.LastIndex(page, "</style>")
	scriptIndex := strings.LastIndex(page, "</script>")
	if styleIndex < 0 || scriptIndex < 0 || strings.Contains(page, "productPolishMarker") {
		return page, false
	}
	page = page[:styleIndex] + productPolishCSS + page[styleIndex:]
	scriptIndex = strings.LastIndex(page, "</script>")
	page = page[:scriptIndex] + productPolishJS + page[scriptIndex:]
	return page, true
}

const productPolishCSS = `
/* productPolishMarker — function-first release polish */
:root{--control-height:44px}
button,input[type=text],input[type=password],input[type=number],input[type=time],select{min-height:var(--control-height)}
button:disabled{transform:none!important;cursor:not-allowed}
.statusbox{overflow-wrap:anywhere}
.first-real-sequence{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:8px;margin:14px 0}
.first-real-stage{position:relative;padding:11px 12px 11px 36px;border:1px solid var(--line);border-radius:12px;background:#fff;color:var(--muted);font-size:13px}
.first-real-stage:before{content:"";position:absolute;left:13px;top:15px;width:10px;height:10px;border-radius:999px;background:#cbd4d9;box-shadow:0 0 0 4px #eef2f4}
.first-real-stage strong{display:block;color:var(--ink);font-size:13px}
.first-real-stage.done{border-color:#cfe9dc;background:#f4fbf8}.first-real-stage.done:before{background:var(--ok);box-shadow:0 0 0 4px #e4f5ed}
.first-real-stage.active{border-color:#b9dbd1;background:#f0faf6}.first-real-stage.active:before{background:var(--accent);box-shadow:0 0 0 4px var(--accent2)}
#firstRealSyncPanel{margin-top:16px!important;border-color:#cfe1db;background:linear-gradient(180deg,#fbfefd,#f5faf8);box-shadow:0 10px 26px rgba(23,107,91,.07)}
#firstRealSyncPanel h3{font-size:18px;margin-bottom:5px}
#firstRealPassword{max-width:520px}
.wizard-footer{backdrop-filter:blur(12px)}
@media(hover:hover) and (pointer:fine){.folder-btn:hover{transform:translateY(-1px)}button:not(:disabled):hover{transform:translateY(-1px)}}
@media(max-width:720px){.wizard-steps{display:flex;overflow-x:auto;overscroll-behavior-inline:contain;scroll-snap-type:x proximity}.step-nav{flex:0 0 auto;min-width:132px;scroll-snap-align:start}.first-real-sequence{grid-template-columns:1fr}.hero{padding:18px}.card{padding:18px}}
@media(max-width:560px){.wizard-steps{display:grid;grid-template-columns:repeat(6,minmax(0,1fr));overflow:visible}.step-nav{min-width:0;justify-content:center;padding-inline:0}.step-nav .step-label{display:none}}
@media(max-width:480px){main{width:min(100% - 18px,1140px)}.wizard-footer{gap:8px}.wizard-position{font-size:12px}.wizard-footer button{padding-inline:12px}.row>button{flex:1 1 auto}}
@media(prefers-reduced-motion:reduce){*,*::before,*::after{animation-duration:.001ms!important;animation-iteration-count:1!important;transition-duration:.001ms!important;scroll-behavior:auto!important}}
@media(forced-colors:active){.hero,.card,.subcard,.setting-card,#firstRealSyncPanel{box-shadow:none}.first-real-stage:before{box-shadow:none}}
`

const productPolishJS = `
(function(){
  const copy=(de,en)=>LANG==="en"?en:de;
  const panel=document.querySelector("#firstRealSyncPanel");
  if(panel&&!document.querySelector("#firstRealSequence")){
    const sequence=document.createElement("div");
    sequence.id="firstRealSequence";
    sequence.className="first-real-sequence";
    sequence.setAttribute("aria-label",copy("Sicherheitsfreigabe","Safety activation"));
    sequence.innerHTML='<div class="first-real-stage done" data-stage="dry"><strong></strong><span></span></div><div class="first-real-stage active" data-stage="real"><strong></strong><span></span></div><div class="first-real-stage" data-stage="auto"><strong></strong><span></span></div>';
    const hint=document.querySelector("#firstRealHint");
    hint?.insertAdjacentElement("afterend",sequence);
  }

  function setStageCopy(){
    const title=document.querySelector("#firstRealTitle");
    if(title)title.textContent=copy("Sichere Freigabe abschließen","Complete safe activation");
    const values={dry:[copy("1 · Testlauf","1 · Dry run"),copy("ohne Upload abgeschlossen","completed without upload")],real:[copy("2 · Echter Lauf","2 · Real run"),copy("bewusst und begrenzt","deliberate and limited")],auto:[copy("3 · Automatik","3 · Automation"),copy("erst danach freigeben","unlock only afterwards")]};
    Object.entries(values).forEach(([key,value])=>{const stage=document.querySelector('[data-stage="'+key+'"]');if(!stage)return;stage.querySelector("strong").textContent=value[0];stage.querySelector("span").textContent=value[1]});
    document.querySelector("#langDE")?.setAttribute("aria-pressed",LANG==="de"?"true":"false");
    document.querySelector("#langEN")?.setAttribute("aria-pressed",LANG==="en"?"true":"false");
  }

  function renderActivationState(state){
    if(!panel)return;
    const real=document.querySelector('[data-stage="real"]');
    const automatic=document.querySelector('[data-stage="auto"]');
    const start=document.querySelector("#firstRealStart");
    const enable=document.querySelector("#firstRealEnable");
    const statusBox=document.querySelector("#firstRealStatus");
    const uploaded=Number(state?.result?.stats?.uploaded||0);

    real?.classList.toggle("done",(state?.status==="completed"&&uploaded>0)||state?.status==="enabled");
    real?.classList.toggle("active",state?.status!=="enabled"&&!(state?.status==="completed"&&uploaded>0));
    automatic?.classList.toggle("active",state?.status==="completed"&&uploaded>0);
    automatic?.classList.toggle("done",state?.status==="enabled");

    if(state?.status==="completed"&&uploaded===0){
      if(start){start.disabled=false;start.textContent=copy("Echten Lauf erneut versuchen","Retry limited real run")}
      if(enable)enable.disabled=true;
      if(statusBox)status(statusBox,copy("Der Lauf war fehlerfrei, hat aber keine Datei hochgeladen. Automatik bleibt gesperrt. Ändere oder ergänze eine passende Datei und starte den begrenzten Lauf erneut.","The run had no errors but uploaded no file. Automation remains locked. Add or change an eligible file and retry the limited run."),"warn");
    }
  }

  async function readProductState(){
    if(!panel)return null;
    try{const data=await api("/api/install/first-real/status");return data.state||{}}catch{return null}
  }
  async function refreshProductState(){const state=await readProductState();if(state)renderActivationState(state)}

  function normalizeStepNavigation(){
    document.querySelectorAll(".step-nav").forEach(button=>{
      const current=button.classList.contains("active");
      if(current)button.setAttribute("aria-current","step");else button.removeAttribute("aria-current");
      if(current&&steps&&steps.scrollWidth>steps.clientWidth){
        steps.scrollLeft=Math.max(0,button.offsetLeft-steps.offsetLeft-(steps.clientWidth-button.clientWidth)/2);
      }
    });
  }
  const steps=document.querySelector("#steps");
  if(steps)new MutationObserver(normalizeStepNavigation).observe(steps,{childList:true,subtree:true,attributes:true,attributeFilter:["class"]});

  document.querySelector("#langDE")?.addEventListener("click",()=>setTimeout(()=>{setStageCopy();refreshProductState()},0));
  document.querySelector("#langEN")?.addEventListener("click",()=>setTimeout(()=>{setStageCopy();refreshProductState()},0));
  document.addEventListener("visibilitychange",()=>{if(!document.hidden)refreshProductState()});
  setStageCopy();normalizeStepNavigation();refreshProductState();
  if(panel)setInterval(()=>{if(!document.hidden)refreshProductState()},1500);
})();
`

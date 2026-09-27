const __vite__mapDeps=(i,m=__vite__mapDeps,d=(m.f||(m.f=["assets/SessionsView-C-ZZy20i.js","assets/vendor-DKEYnvms.js","assets/query-rbaMx2ep.js","assets/sessions-Ccqb83NA.js","assets/format-ntNSetjV.js","assets/SessionsView-C6f1ATbz.css","assets/SessionDetailView-LcLMFC9X.js","assets/render-DGjdWCNV.js","assets/SessionDetailView-XCuft0BO.css","assets/PluginsView-C-3XM6_T.js","assets/refresh-cw-5jN3xXVy.js","assets/PluginsView-C4nwxZLB.css","assets/ModelsView-BffKemkg.js","assets/ModelsView-Da7TLeIK.css","assets/TasksView-B1oqmejW.js","assets/x-D1mMUyxV.js","assets/ApprovalsView-SvrtXy1Q.js","assets/ApprovalsView-CRQ0Dofn.css"])))=>i.map(i=>d[i]);
import{f as A,r as w,g as O,j as E,k as V,l as k,m as l,p as q,v as z,q as N,u as I,x as b,y as M,F as R,z as D,A as B,B as P,C as f,D as j,E as H,G as K,H as $,I as U,J as F,K as Q,L as G}from"./vendor-DKEYnvms.js";import{Q as W,V as J}from"./query-rbaMx2ep.js";(function(){const o=document.createElement("link").relList;if(o&&o.supports&&o.supports("modulepreload"))return;for(const t of document.querySelectorAll('link[rel="modulepreload"]'))a(t);new MutationObserver(t=>{for(const n of t)if(n.type==="childList")for(const r of n.addedNodes)r.tagName==="LINK"&&r.rel==="modulepreload"&&a(r)}).observe(document,{childList:!0,subtree:!0});function s(t){const n={};return t.integrity&&(n.integrity=t.integrity),t.referrerPolicy&&(n.referrerPolicy=t.referrerPolicy),t.crossOrigin==="use-credentials"?n.credentials="include":t.crossOrigin==="anonymous"?n.credentials="omit":n.credentials="same-origin",n}function a(t){if(t.ep)return;t.ep=!0;const n=s(t);fetch(t.href,n)}})();const Y={},L="dsh.console.token",T=A("token",()=>{const e=w(Z());function o(a){e.value=a,a?localStorage.setItem(L,a):localStorage.removeItem(L)}function s(){o("")}return{token:e,setToken:o,clearToken:s}});function Z(){try{const o=localStorage.getItem(L);if(o)return o}catch{}const e=Y?.VITE_AUTH_TOKEN;return typeof e=="string"&&e?e:""}const C=O.create({baseURL:"/api/v1/console",timeout:3e4,headers:{"Content-Type":"application/json"}});C.interceptors.request.use(e=>{const o=T();return o.token&&(e.headers.Authorization=`Bearer ${o.token}`),e});C.interceptors.response.use(e=>e,e=>{const o=e.response?.data?.error?.message||e.message||"请求失败";return Promise.reject(new Error(o))});/**
 * @license lucide-vue-next v0.469.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const X=e=>e.replace(/([a-z0-9])([A-Z])/g,"$1-$2").toLowerCase();/**
 * @license lucide-vue-next v0.469.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */var v={xmlns:"http://www.w3.org/2000/svg",width:24,height:24,viewBox:"0 0 24 24",fill:"none",stroke:"currentColor","stroke-width":2,"stroke-linecap":"round","stroke-linejoin":"round"};/**
 * @license lucide-vue-next v0.469.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const ee=({size:e,strokeWidth:o=2,absoluteStrokeWidth:s,color:a,iconNode:t,name:n,class:r,...c},{slots:h})=>E("svg",{...v,width:e||v.width,height:e||v.height,stroke:a||v.stroke,"stroke-width":s?Number(o)*24/Number(e):o,class:["lucide",`lucide-${X(n??"icon")}`],...c},[...t.map(i=>E(...i)),...h.default?[h.default()]:[]]);/**
 * @license lucide-vue-next v0.469.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const _=(e,o)=>(s,{slots:a})=>E(ee,{...s,iconNode:o,name:e},a);/**
 * @license lucide-vue-next v0.469.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const te=_("CpuIcon",[["rect",{width:"16",height:"16",x:"4",y:"4",rx:"2",key:"14l7u7"}],["rect",{width:"6",height:"6",x:"9",y:"9",rx:"1",key:"5aljv4"}],["path",{d:"M15 2v2",key:"13l42r"}],["path",{d:"M15 20v2",key:"15mkzm"}],["path",{d:"M2 15h2",key:"1gxd5l"}],["path",{d:"M2 9h2",key:"1bbxkp"}],["path",{d:"M20 15h2",key:"19e6y8"}],["path",{d:"M20 9h2",key:"19tzq7"}],["path",{d:"M9 2v2",key:"165o2o"}],["path",{d:"M9 20v2",key:"i2bqo8"}]]);/**
 * @license lucide-vue-next v0.469.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const oe=_("ListTodoIcon",[["rect",{x:"3",y:"5",width:"6",height:"6",rx:"1",key:"1defrl"}],["path",{d:"m3 17 2 2 4-4",key:"1jhpwq"}],["path",{d:"M13 6h8",key:"15sg57"}],["path",{d:"M13 12h8",key:"h98zly"}],["path",{d:"M13 18h8",key:"oe0vm4"}]]);/**
 * @license lucide-vue-next v0.469.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const se=_("LogOutIcon",[["path",{d:"M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4",key:"1uf3rs"}],["polyline",{points:"16 17 21 12 16 7",key:"1gabdz"}],["line",{x1:"21",x2:"9",y1:"12",y2:"12",key:"1uyos4"}]]);/**
 * @license lucide-vue-next v0.469.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const ne=_("MessageSquareIcon",[["path",{d:"M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z",key:"1lielz"}]]);/**
 * @license lucide-vue-next v0.469.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const ae=_("PackageIcon",[["path",{d:"M11 21.73a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73z",key:"1a0edw"}],["path",{d:"M12 22V12",key:"d0xqtd"}],["path",{d:"m3.3 7 7.703 4.734a2 2 0 0 0 1.994 0L20.7 7",key:"yx3hmr"}],["path",{d:"m7.5 4.27 9 5.15",key:"1c824w"}]]);/**
 * @license lucide-vue-next v0.469.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const re=_("ShieldCheckIcon",[["path",{d:"M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z",key:"oel41y"}],["path",{d:"m9 12 2 2 4-4",key:"dzmm74"}]]),ce={class:"layout"},ie={class:"topbar"},le={class:"token-form"},ue={key:1,class:"badge badge-loaded"},de={key:2,class:"badge badge-failed"},pe={class:"body"},he={class:"sidebar"},me={class:"content"},fe=V({__name:"App",setup(e){const o=B(),s=T(),a=w(s.token),t=w("unknown");async function n(){s.setToken(a.value.trim()),await r()}async function r(){try{await C.get("/health"),t.value="ok"}catch{t.value="failed"}}function c(){s.clearToken(),a.value="",t.value="unknown",o.push("/sessions")}const h=[{to:"/sessions",label:"会话",icon:ne},{to:"/plugins",label:"插件",icon:ae},{to:"/models",label:"模型",icon:te},{to:"/tasks",label:"任务",icon:oe},{to:"/approvals",label:"审批",icon:re}];return(i,u)=>{const m=P("router-link"),d=P("router-view");return f(),k("div",ce,[l("header",ie,[u[1]||(u[1]=l("div",{class:"brand"},[l("span",{class:"logo"},"⚡"),l("span",{class:"title"},"DeepSeek Harness · 控制台"),l("span",{class:"version"},"v8.0.0-dev")],-1)),l("div",le,[q(l("input",{"onUpdate:modelValue":u[0]||(u[0]=p=>a.value=p),type:"text",placeholder:"Bearer token",onKeyup:N(n,["enter"])},null,544),[[z,a.value]]),l("button",{class:"btn",onClick:n},"连接"),I(s).token?(f(),k("button",{key:0,class:"btn",onClick:c,title:"登出"},[b(I(se),{size:14})])):M("",!0),t.value==="ok"?(f(),k("span",ue,"已连接")):t.value==="failed"?(f(),k("span",de,"认证失败")):M("",!0)])]),l("div",pe,[l("aside",he,[l("nav",null,[(f(),k(R,null,D(h,p=>b(m,{key:p.to,to:p.to,class:"nav-item","active-class":"active"},{default:j(()=>[(f(),H(K(p.icon),{size:16})),l("span",null,$(p.label),1)]),_:2},1032,["to"])),64))])]),l("main",me,[b(d)])])])}}}),ye=(e,o)=>{const s=e.__vccOpts||e;for(const[a,t]of o)s[a]=t;return s},_e=ye(fe,[["__scopeId","data-v-c13207ac"]]),ke="modulepreload",ve=function(e){return"/"+e},S={},y=function(o,s,a){let t=Promise.resolve();if(s&&s.length>0){let r=function(i){return Promise.all(i.map(u=>Promise.resolve(u).then(m=>({status:"fulfilled",value:m}),m=>({status:"rejected",reason:m}))))};document.getElementsByTagName("link");const c=document.querySelector("meta[property=csp-nonce]"),h=c?.nonce||c?.getAttribute("nonce");t=r(s.map(i=>{if(i=ve(i),i in S)return;S[i]=!0;const u=i.endsWith(".css"),m=u?'[rel="stylesheet"]':"";if(document.querySelector(`link[href="${i}"]${m}`))return;const d=document.createElement("link");if(d.rel=u?"stylesheet":ke,u||(d.as="script"),d.crossOrigin="",d.href=i,h&&d.setAttribute("nonce",h),document.head.appendChild(d),u)return new Promise((p,x)=>{d.addEventListener("load",p),d.addEventListener("error",()=>x(new Error(`Unable to preload CSS for ${i}`)))})}))}function n(r){const c=new Event("vite:preloadError",{cancelable:!0});if(c.payload=r,window.dispatchEvent(c),!c.defaultPrevented)throw r}return t.then(r=>{for(const c of r||[])c.status==="rejected"&&n(c.reason);return o().catch(n)})},ge=[{path:"/",redirect:"/sessions"},{path:"/sessions",name:"sessions",component:()=>y(()=>import("./SessionsView-C-ZZy20i.js"),__vite__mapDeps([0,1,2,3,4,5]))},{path:"/sessions/:sid",name:"session-detail",component:()=>y(()=>import("./SessionDetailView-LcLMFC9X.js"),__vite__mapDeps([6,1,2,3,7,8])),props:!0},{path:"/plugins",name:"plugins",component:()=>y(()=>import("./PluginsView-C-3XM6_T.js"),__vite__mapDeps([9,2,1,10,11]))},{path:"/models",name:"models",component:()=>y(()=>import("./ModelsView-BffKemkg.js"),__vite__mapDeps([12,2,1,10,13]))},{path:"/tasks",name:"tasks",component:()=>y(()=>import("./TasksView-B1oqmejW.js"),__vite__mapDeps([14,1,2,4,10,15]))},{path:"/approvals",name:"approvals",component:()=>y(()=>import("./ApprovalsView-SvrtXy1Q.js"),__vite__mapDeps([16,2,1,4,10,15,17]))},{path:"/:pathMatch(.*)*",redirect:"/sessions"}],be=U({history:F("/console/"),routes:ge}),we=new W({defaultOptions:{queries:{staleTime:3e4,gcTime:5*6e4,retry:1,refetchOnWindowFocus:!1}}}),g=Q(_e);g.use(G());g.use(be);g.use(J,{queryClient:we});g.mount("#app");export{oe as L,ne as M,ae as P,re as S,ye as _,C as a,_ as c};

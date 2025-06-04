import{u as i,j as a}from"./index-Bqsuz-xJ.js";import{M as m,z as p}from"./vendor-ui-BgnAtmuh.js";import{r as l}from"./vendor-core-Cler3e7Q.js";import"./vendor-utils-Dg5_7-NF.js";const g=({record:o,visible:s,onClose:n})=>{const{t:c}=i(),r=l.useRef(null),t=l.useRef(o.log);return l.useEffect(()=>{r.current&&t.current!==o.log&&(t.current=o.log,requestAnimationFrame(()=>{r.current&&(r.current.scrollTop=r.current.scrollHeight)}))},[o.log]),l.useEffect(()=>{const e=document.createElement("style");return e.textContent=`
			.custom-scrollbar::-webkit-scrollbar {
				width: 8px;
				height: 8px;
			}
			.custom-scrollbar::-webkit-scrollbar-track {
				background: #1e1e1e;
				border-radius: 4px;
			}
			.custom-scrollbar::-webkit-scrollbar-thumb {
				background: #4a4a4a;
				border-radius: 4px;
			}
			.custom-scrollbar::-webkit-scrollbar-thumb:hover {
				background: #5a5a5a;
			}
			.custom-modal .ant-modal-content {
				background-color: #1e1e1e;
				border-radius: 8px;
				height: 80vh;
				display: flex;
				flex-direction: column;
			}
			.custom-modal .ant-modal-header {
				background-color: #1e1e1e;
				border-bottom: 1px solid #333;
				padding: 16px 24px;
				flex-shrink: 0;
			}
			.custom-modal .ant-modal-title {
				color: #e5e7eb;
			}
			.custom-modal .ant-modal-close {
				color: #6b7280;
			}
			.custom-modal .ant-modal-close:hover {
				color: #e5e7eb;
			}
			.custom-modal .ant-modal-body {
				padding: 0;
				flex: 1;
				overflow: hidden;
			}
			.custom-modal-wrapper .ant-modal-mask {
				background-color: rgba(0, 0, 0, 0.75);
			}
		`,document.head.appendChild(e),()=>{document.head.removeChild(e)}},[]),a.jsx(m,{title:c("sys.cicd.deployLogs"),open:s,onCancel:n,width:900,centered:!0,bodyStyle:{padding:"0",backgroundColor:"#1e1e1e",borderRadius:"0 0 8px 8px",height:"calc(80vh - 55px)"},maskStyle:{backgroundColor:"rgba(0, 0, 0, 0.75)"},className:"custom-modal",wrapClassName:"custom-modal-wrapper",footer:null,children:a.jsx("div",{ref:r,style:{height:"100%",overflow:"auto",padding:"16px",backgroundColor:"#1e1e1e",borderRadius:"6px",fontSize:"13px",lineHeight:"1.6",fontFamily:'Consolas, Monaco, "Courier New", monospace',scrollbarWidth:"thin",scrollbarColor:"#4a4a4a #1e1e1e"},className:"custom-scrollbar",children:o.log.map((e,d)=>a.jsxs("div",{style:{marginBottom:"4px",display:"flex",alignItems:"flex-start",gap:"12px",padding:"4px 8px",borderRadius:"4px",backgroundColor:e.level==="error"?"rgba(239, 68, 68, 0.1)":e.level==="warn"?"rgba(245, 158, 11, 0.1)":e.level==="light"?"rgba(34, 197, 94, 0.1)":"transparent",transition:"background-color 0.2s"},children:[a.jsx("span",{style:{color:"#6b7280",flexShrink:0,fontFamily:"monospace",fontSize:"12px",paddingTop:"2px"},children:p(e.time).format("YYYY-MM-DD HH:mm:ss")}),a.jsx("span",{style:{color:e.level==="error"?"#ef4444":e.level==="warn"?"#f59e0b":e.level==="light"?"#22c55e":"#e5e7eb",flex:1,whiteSpace:"pre-wrap",wordBreak:"break-all",paddingTop:"2px"},children:e.text})]},`${e.time}-${d}`))})})};export{g as default};

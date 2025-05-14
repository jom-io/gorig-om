import{j as e,t as a,S as r}from"./index-DM01Klox.js";import{s as c,a6 as t,H as l,T as w,ac as y,z as n,Y as i,I as T,a3 as z,d as D}from"./vendor-ui-BgnAtmuh.js";import"./vendor-core-Cler3e7Q.js";import"./vendor-utils-Dg5_7-NF.js";function M({task:x}){const{title:o,reporter:h,assignee:m=[],tags:j=[],date:p,priority:f,description:u,attachments:v,comments:g=[]}=x;return e.jsxs(e.Fragment,{children:[e.jsxs(H,{children:[e.jsx("div",{className:"item",children:e.jsx(c.Title,{level:4,children:o})}),e.jsxs("div",{className:"item",children:[e.jsx("div",{className:"label",children:"Reporter"}),e.jsx(t,{shape:"circle",src:h,size:40})]}),e.jsxs("div",{className:"item",children:[e.jsx("div",{className:"label",children:"Assignee"}),e.jsx(l,{children:m.map(s=>e.jsx(t,{shape:"circle",src:s,size:40},s))})]}),e.jsxs("div",{className:"item",children:[e.jsx("div",{className:"label",children:"Tag"}),e.jsx(l,{wrap:!0,children:j.map(s=>e.jsx(w,{color:a.colors.palette.info.default,children:s},s))})]}),e.jsxs("div",{className:"item",children:[e.jsx("div",{className:"label",children:"Due Date"}),e.jsx(y,{variant:"borderless",value:n(p)})]}),e.jsxs("div",{className:"item",children:[e.jsx("div",{className:"label",children:"Priority"}),e.jsx("div",{children:e.jsx(i.Group,{defaultValue:f,children:e.jsxs(l,{children:[e.jsxs(i.Button,{value:"High",children:[e.jsx(r,{icon:"ic_rise",size:20,color:a.colors.palette.warning.default}),e.jsx("span",{children:"High"})]}),e.jsxs(i.Button,{value:"Medium",children:[e.jsx(r,{icon:"ic_rise",size:20,color:a.colors.palette.success.default,className:"rotate-90"}),e.jsx("span",{children:"Medium"})]}),e.jsxs(i.Button,{value:"Low",children:[e.jsx(r,{icon:"ic_rise",size:20,color:a.colors.palette.info.default,className:"rotate-180"}),e.jsx("span",{children:"Low"})]})]})})})]}),e.jsxs("div",{className:"item",children:[e.jsx("div",{className:"label",children:"Description"}),e.jsx(T.TextArea,{defaultValue:u})]}),e.jsxs("div",{className:"item",children:[e.jsx("div",{className:"label",children:"Attachments"}),e.jsx(l,{wrap:!0,children:v?.map(s=>e.jsx(z,{src:s,width:62,height:62,className:"rounded-lg"},s))})]})]}),e.jsx("div",{className:"flex flex-col gap-4",style:{padding:"24px 20px 40px"},children:g?.map(({avatar:s,username:d,content:N,time:b})=>e.jsxs("div",{className:"flex gap-4",children:[e.jsx(t,{src:s,size:40,className:"flex-shrink-0"}),e.jsxs("div",{className:"flex flex-grow flex-col flex-wrap gap-1 text-gray",children:[e.jsxs("div",{className:"flex justify-between",children:[e.jsx(c.Text,{children:d}),e.jsx(c.Text,{children:n(b).format("DD/MM/YYYY HH:mm")})]}),e.jsx("p",{children:N})]})]},d))})]})}const H=D.div`
  display: flex;
  flex-direction: column;
  gap: 24px;
  padding: 24px 20px 40px;
  .item {
    display: flex;
    align-items: center;
  }
  .label {
    text-align: left;
    font-size: 0.75rem;
    font-weight: 600;
    width: 100px;
    flex-shrink: 0;
    color: rgb(99, 115, 129);
    height: 40px;
    line-height: 40px;
  }
`;export{M as default};

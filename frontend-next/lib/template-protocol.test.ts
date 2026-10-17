// Runs with: node --experimental-strip-types --test lib/template-protocol.test.ts
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { templatePrompt, parseTemplateResponse } from './template-protocol.ts';
const fields = { agency:'ỦY BAN NHÂN DÂN TỈNH', locality:'Hà Tĩnh', date:'ngày 16 tháng 10 năm 2026', number_symbol:'DỰ THẢO', subject:'Kế hoạch đào tạo', paragraphs:[{kind:'body',text:'Tổ chức đào tạo.'}], recipients:['Các đơn vị;'], signature_lines:['CHỦ TỊCH'], signer_name:'Nguyễn Văn A' };
test('prefill invites extra information without silently sending',()=>{
 assert.equal(templatePrompt('ke-hoach'),'Tạo cho tôi văn bản kế hoạch theo NĐ 30, thông tin tôi cần là: ');
});
test('accepts only versioned typed responses, not prose or executable operations',()=>{
 const raw={schema_version:1,request_id:'req1',document_id:'doc1',template_id:'ke-hoach',status:'ready',fields};
 assert.equal(parseTemplateResponse(raw)?.status,'ready');
 assert.equal(parseTemplateResponse({...raw,fields:{...fields,agency:''}}),null);
 assert.equal(parseTemplateResponse({...raw,fields:{...fields,paragraphs:[]}}),null);
 assert.equal(parseTemplateResponse({...raw,schema_version:2}),null);
 assert.equal(parseTemplateResponse({...raw,template_id:'unknown'}),null);
 assert.equal(parseTemplateResponse(JSON.stringify(raw)),null);
 assert.equal(parseTemplateResponse({...raw,status:'needs_input',fields:undefined,missing_fields:['Mục đích']} )?.status,'needs_input');
});

test('pending requests survive pane reload only in the matching tenant/session/document', async()=>{
 const { saveTemplateRequest,restoreTemplateRequest,forgetTemplateRequest }=await import('./template-protocol.ts');
 const map=new Map<string,string>();const store={getItem:(k:string)=>map.get(k)??null,setItem:(k:string,v:string)=>{map.set(k,v);},removeItem:(k:string)=>{map.delete(k);}};
 const pending={schema_version:1 as const,request_id:'r1',template_id:'ke-hoach' as const,document_id:'doc1',sessionId:'s1'};
 saveTemplateRequest(store,'t1',pending);assert.deepEqual(restoreTemplateRequest(store,'t1','s1','doc1'),pending);
 assert.equal(restoreTemplateRequest(store,'t2','s1','doc1'),null);assert.equal(restoreTemplateRequest(store,'t1','s2','doc1'),null);
 const newer={...pending,request_id:'r2'};saveTemplateRequest(store,'t1',newer);forgetTemplateRequest(store,'t1',pending);assert.deepEqual(restoreTemplateRequest(store,'t1','s1','doc1'),newer);
 forgetTemplateRequest(store,'t1',newer);assert.equal(restoreTemplateRequest(store,'t1','s1','doc1'),null);
});

test('a plain chat "draft it again" names the type to redo', async()=>{
 const { redoTemplateId }=await import('./template-protocol.ts');
 assert.equal(redoTemplateId('Tạo lại công văn nhé',null),'cong-van');
 assert.equal(redoTemplateId('soạn lại quyết định giúp tôi','cong-van'),'quyet-dinh');
 assert.equal(redoTemplateId('Làm lại văn bản','to-trinh'),'to-trinh');
 assert.equal(redoTemplateId('Tạo lại văn bản',null),null);
 assert.equal(redoTemplateId('tạo lại sơ đồ','cong-van'),null);
 assert.equal(redoTemplateId('Tạo công văn mới gửi PV01','cong-van'),null);
 assert.equal(redoTemplateId('tạo lạinh công văn',null),null);
});

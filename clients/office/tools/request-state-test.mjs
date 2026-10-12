import assert from 'node:assert/strict';
for(const app of ['powerpoint','word','excel']) {
 const {Transcript}=await import(`../../${app}/addin/src/domain/Transcript.js`);
 const {userBadge}=await import(`../../${app}/addin/src/ui/screen.js`);
 const log=new Transcript();
 const user=(seq,id,from)=>({seq,type:'prompt.submitted',actor:{kind:'user'},data:{messageId:id,parts:[{kind:'text',text:'test'}],...(from?{resurfacedFrom:from}:{})}});
 log.append(user(1,'first'));log.append(user(2,'second'));
 assert.equal(log.rows[1].status,'queued');
 log.append({seq:3,type:'error',data:{message:'HTTP 404',code:'provider'}});
 log.append(user(4,'retry','second'));
 log.append({seq:5,type:'turn.finished',data:{}});
 assert.equal(log.rows[0].status,'failed');
 assert.equal(log.rows.find(r=>r.messageId==='retry').status,'pending');
 assert.ok(!userBadge('pending').text.includes('지금 일이 끝나면'));
 log.append(user(6,'new'));
 assert.equal(log.rows.find(r=>r.messageId==='new').status,'running');
 console.log(`PASS ${app}: provider error marks failed, queued retry after finish is pending, new request can run`);
}

import assert from 'node:assert/strict';
const element = () => ({ hidden:false, disabled:false, style:{}, attrs:{}, setAttribute(k,v){this.attrs[k]=v;}, replaceChildren(){}, appendChild(){} });
for (const app of ['powerpoint','word','excel']) {
 const {View} = await import(`../../${app}/addin/src/ui/view.js`);
 const nodes = new Map(['#busy','#compact','#stop','#ctx','#ctx-bar'].map(k=>[k,element()]));
 globalThis.document = {querySelector:k=>nodes.get(k)??null,createElement:element};
 const view = Object.create(View.prototype);
 view.folding(true);
 view.renderBusy([]);
 view.contextMeter({used:100,window:1000,parts:{talk:100}});
 assert.equal(nodes.get('#compact').disabled,true,app);
 assert.equal(nodes.get('#busy').hidden,false,app);
 view.folding(false);
 assert.equal(nodes.get('#compact').disabled,false,app);
 assert.equal(nodes.get('#busy').hidden,true,app);
 view._turnRunning=true;
 view.folding(true);
 view.folding(false);
 assert.equal(nodes.get('#busy').hidden,false,app);
 assert.equal(nodes.get('#busy').attrs['aria-label'],'답변 생성 중',app);
 console.log(`${app}: compaction and turn activity stay consistent`);
}

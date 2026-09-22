const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const source = fs.readFileSync(path.join(__dirname, '../internal/web/assets/i18n.js'), 'utf8');
function setup(navigator, before = '') {
  const document = {documentElement: {}, querySelectorAll: () => []};
  const events = [];
  const window = {dispatchEvent: event => events.push(event)};
  vm.runInNewContext(before + source, {navigator, document, window, queueMicrotask, CustomEvent: class {constructor(type, options) {this.type=type;this.detail=options.detail;}}});
  return {...window.smartStageI18n, api: window.smartStageI18n, document, events};
}
test('system locale resolves regional languages, empty preference lists and unsupported languages', () => {
  for (const [navigator, expected] of [
    [{languages:['it-IT'],language:'en-US'},'it'],
    [{languages:['en-GB','it-IT'],language:'it-IT'},'en'],
    [{languages:['it_IT']},'it'],
    [{languages:[],language:'it-CH'},'it'],
    [{language:'it'},'it'],
    [{languages:['de-DE'],language:'de-DE'},'en']
  ]) assert.equal(setup(navigator).api.language, expected);
});
test('catalog formats Italian UI text while retaining raw diagnostic details', () => {
  const {api} = setup({language:'it'});
  assert.equal(api.t('Quit Smart Stage'), 'Esci da Smart Stage');
  assert.equal(api.t('{0} cue · saved revision {1}', {0:1,1:3}), '1 elemento · revisione salvata 3');
  const raw = '/Show/Color {file}.mp3: errno=13';
  assert.equal(api.diagnostic('Update check failed: '+raw), 'Ricerca degli aggiornamenti non riuscita: '+raw);
  assert.equal(api.t(raw), raw);
  api.setLanguage('en'); assert.equal(api.t('Quit Smart Stage'), 'Quit Smart Stage');
});
test('explicit bindings prune detached nodes without waiting for a language change', async () => {
  const {api} = setup({language:'en'});
  const live = {isConnected:true, textContent:''};
  api.text(live, () => api.t('Play'));
  for (let batch=0;batch<10;batch++) {
    for (let i=0;i<100;i++) { const removed={isConnected:true}; api.text(removed,()=>api.t('Remove')); removed.isConnected=false; }
    await new Promise(resolve => queueMicrotask(resolve));
    assert.equal(api.bindingCount, 1);
  }
  api.setLanguage('it'); assert.equal(live.textContent,'Riproduci');
});

test('catalog works on Safari-era engines without Object.hasOwn', () => {
  const {api} = setup({language:'it'}, 'Object.hasOwn = undefined;\n');
  assert.equal(api.t('Play'),'Riproduci');
  assert.equal(api.t('{0} cue · saved revision {1}',{0:1,1:7}),'1 elemento · revisione salvata 7');
  assert.equal(api.t('toString'),'toString');
});

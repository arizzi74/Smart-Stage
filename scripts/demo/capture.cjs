'use strict';
// Capture the real embedded Admin UI. No fake HTML/DOM styling, OS-dialog
// artwork, media playback, file-upload endpoint or public gateway is involved.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const crypto = require('node:crypto');
const {spawn, spawnSync} = require('node:child_process');
const {chromium} = require('../browser/node_modules/playwright');
const root = path.resolve(__dirname, '../..');
const output = path.join(root, 'docs/site/assets/demos');
const requested = process.argv.slice(2);
const languages = requested.length ? requested : ['en', 'it'];
assert(languages.every(language => ['en', 'it'].includes(language)), 'Languages: en it');
const work = fs.mkdtempSync(path.join(os.tmpdir(), 'smartstage-tutorial-'));
const fixture = path.join(work, 'fixture.test');
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));
const sha = data => crypto.createHash('sha256').update(data).digest('hex');
function run(command,args,options={}){
  const result=spawnSync(command,args,{cwd:root,encoding:'utf8',...options});
  assert.equal(result.status,0,`${command} failed: ${result.stderr}\n${result.stdout}`);
  return result.stdout.trim();
}
async function waitUntil(predicate, description, timeout=30000){
  const deadline=Date.now()+timeout;
  while(Date.now()<deadline){if(await predicate())return;await sleep(50);}
  throw new Error('Timed out: '+description);
}
(async()=>{
 fs.mkdirSync(output,{recursive:true});
 const overlay=path.join(work,'overlay.json');
 fs.writeFileSync(overlay,JSON.stringify({Replace:{[path.join(root,'internal/httpapi/zz_website_tutorial_test.go')]:path.join(__dirname,'fixture_test.go.txt')}}));
 const assetFiles=fs.readdirSync(path.join(root,'internal/web/assets')).map(name=>path.join(root,'internal/web/assets',name)).filter(file=>fs.statSync(file).isFile());
 const assetHashes=()=>Object.fromEntries(assetFiles.map(file=>[path.relative(root,file),sha(fs.readFileSync(file))]));
 const embeddedHashes=assetHashes();
 run('go',['test','-c','-overlay='+overlay,'./internal/httpapi','-o',fixture]);
 assert.deepEqual(assetHashes(),embeddedHashes,'App assets changed while compiling the capture fixture; rerun after edits finish');
 const browser=await chromium.launch({headless:true});
 const records=[];
 try{
  for(const language of languages){
   for(const scenario of ['add-media','outputs','remote',...(language==='it'?['overview']:[])]){
    const directory=path.join(work,language+'-'+scenario);fs.mkdirSync(directory);
    const log=fs.openSync(path.join(directory,'fixture.log'),'w');
    const server=spawn(fixture,['-test.run=^TestWebsiteTutorialFixture$','-test.timeout=6m'],{cwd:root,env:{...process.env,SMARTSTAGE_DEMO_WORK:directory,SMARTSTAGE_DEMO_LANGUAGE:language,SMARTSTAGE_DEMO_SCENARIO:scenario},stdio:['ignore',log,log]});
    let context;
    try{
     await waitUntil(()=>fs.existsSync(path.join(directory,'server.json')),'fixture ready');
     const {admin,remote}=JSON.parse(fs.readFileSync(path.join(directory,'server.json'),'utf8'));
     const viewport=scenario==='overview'?{width:1400,height:1070}:scenario==='remote'?{width:1040,height:1600}:{width:780,height:1080};
     context=await browser.newContext({viewport,deviceScaleFactor:1,locale:language==='it'?'it-IT':'en-US',userAgent:'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/153.0.0.0 Safari/537.36 SmartStageDesktop'});
     const page=await context.newPage();const errors=[];page.on('pageerror',error=>errors.push(error.message));
     await page.goto(admin,{waitUntil:'domcontentloaded'});
     await page.locator('#connection.live').waitFor();
     if(await page.locator('#language-mode').count()){
      await page.locator('#language-mode').selectOption(language);
      await page.waitForFunction(lang=>document.documentElement.lang===lang,language);
     }else assert.equal(language,'en','Italian production localization is not available');
     await page.waitForTimeout(300);
     const target=['add-media','overview'].includes(scenario)?'#playlist-section':scenario==='outputs'?'#outputs-section':'#remote-section';
     await page.locator(`.section-nav a[href="${target}"]`).click();
     await page.waitForTimeout(200);
     async function framePanel(){
      await page.evaluate(selector=>{
       const panel=document.querySelector(selector).getBoundingClientRect();
       const header=document.querySelector('.transport').getBoundingClientRect();
       window.scrollBy(0,panel.top-header.bottom-24);
      },target);
     }
     await framePanel();
     if(scenario==='overview'){
      await page.locator('.playlist-row').nth(5).waitFor();
      await page.screenshot({path:path.join(output,'overview-admin-it.png')});
      const phone=await context.newPage();
      phone.on('pageerror',error=>errors.push(error.message));
      await phone.setViewportSize({width:430,height:860});
      await phone.goto(remote,{waitUntil:'domcontentloaded'});
      await phone.locator('#connection.live').waitFor();
      await phone.locator('#language-mode').selectOption('it');
      await phone.waitForFunction(()=>document.documentElement.lang==='it');
      await phone.waitForTimeout(300);
      await phone.screenshot({path:path.join(output,'overview-remote-it.png')});
      assert.deepEqual(errors,[],'Overview browser script errors');
      records.push({language,scenario,files:[{name:'overview-admin-it.png',width:1400,height:1070},{name:'overview-remote-it.png',width:430,height:860}].map(({name,width,height})=>({path:name,width,height,sha256:sha(fs.readFileSync(path.join(output,name)))}))});
      console.log('Captured Italian Admin and remote overviews');
      continue;
     }
     const frames=[];
     async function capture(duration=250){
      await framePanel();
      const panel=await page.locator(target).boundingBox();
      const clip={x:Math.floor(panel.x),y:Math.floor(panel.y),width:Math.floor(panel.width),height:Math.ceil(panel.height)};
      assert(clip.y>=0 && clip.y+clip.height<=viewport.height,JSON.stringify({clip,viewport}));
      const name=`${String(frames.length).padStart(3,'0')}.png`;
      await page.screenshot({path:path.join(directory,name),clip});
      frames.push({file:name,duration});
     }
     async function hold(ms){for(let n=0;n<Math.ceil(ms/250);n++){await capture(250);await sleep(100);}}
     await hold(1000);
     if(scenario==='add-media'){
      await page.locator('#choose-files').focus();await hold(500);
      await page.locator('#choose-files').click();await hold(1000);
      await page.locator('.playlist-row').first().waitFor();
      await page.waitForFunction(async()=>{const result=await api('GET','/api/state');return result.state.cues.length===1&&result.state.cues[0].validation==='ready';});
      await hold(750);
      const title=language==='it'?'Video di benvenuto':'Welcome film';
      const label=page.locator('#playlist .playlist-info > input').first();
      await label.focus();await hold(500);
      await label.fill(title);await hold(500);
      await label.press('Tab');await hold(750);
      const color=page.locator('#playlist input[type="color"]').first();
      await color.focus();await hold(250);
      await color.fill('#5567d9');await hold(500);
      await page.waitForFunction(async expected=>{const result=await api('GET','/api/playlist');return result.cues.length===1&&result.cues[0].label===expected&&result.cues[0].color==='#5567d9';},title);
      await hold(1750);
      assert.equal(await page.locator('.playlist-row').count(),1);
     }else if(scenario==='outputs'){
      await page.locator('#audio-output').focus();await hold(500);
      await page.locator('#audio-output').selectOption('venue');await hold(750);
      await page.locator('#display-output').focus();await hold(500);
      await page.locator('#display-output').selectOption('projector');await hold(750);
      await page.locator('#save-outputs').click();await hold(1750);
      const saved=await page.evaluate(async()=>await api('GET','/api/playlist'));
      assert.equal(saved.outputs.audioId,'venue');assert.equal(saved.outputs.displayId,'projector');
     }else{
      await page.locator('#remote-connection-settings > summary').click();await hold(500);
      await page.locator('#gateway-url').fill('https://stage.example.com/smartstage');await hold(750);
      await page.locator('#gateway-token').fill('0'.repeat(64));await hold(500);
      await page.locator('#save-gateway').click();await hold(750);
      await page.locator('#remote-ready').waitFor();
      await page.locator('#remote-connection-settings > summary').click();
      await page.waitForFunction(()=>document.getElementById('remote-qr').naturalWidth>100);
      await hold(2250);
      assert((await page.locator('#remote-url').textContent()).startsWith('https://stage.example.com/'));
     }
     assert.deepEqual(errors,[],'Browser script errors');
     fs.writeFileSync(path.join(directory,'frames.json'),JSON.stringify({frames},null,2));
     const name=`${scenario}-${language}`;
     const encoded=JSON.parse(run('python3',[path.join(__dirname,'encode.py'),directory,path.join(output,name)]));
     const files=['gif','png'].map(extension=>({path:`${name}.${extension}`,sha256:sha(fs.readFileSync(path.join(output,`${name}.${extension}`)))}));
     records.push({language,scenario,...encoded,files});
     if(language==='it')await page.screenshot({path:path.join(work,`italian-${scenario}-full.png`),fullPage:true});
     console.log(JSON.stringify({name,...encoded}));
    }finally{
     if(context)await context.close();
     if(server.exitCode===null){const exited=new Promise(resolve=>server.once('exit',resolve));server.kill('SIGTERM');await exited;}fs.closeSync(log);
    }
   }
  }
 }finally{await browser.close();}
 assert.deepEqual(assetHashes(),embeddedHashes,'App assets changed during capture; rerun after edits finish');
 const report={sourceRevision:run('git',['rev-parse','HEAD']),sourceRevisionMeaning:'Git HEAD at capture time. Embedded asset hashes identify the exact working-tree UI, including any uncommitted changes.',browser:browser.version(),method:'Real embedded Admin HTML/CSS/JavaScript, production Go HTTP API and app service; Playwright interactions; relevant panel screenshots resized and padded to a stable canvas, then encoded with a common GIF palette. No DOM overrides or fabricated operating-system dialogs.',scope:'Documentation tutorial with synthetic native media validation/devices, simulated native chooser completion and simulated public gateway registration. No hardware playback, native OS chooser or external/public service was tested. Example.com and all-zero tokens are nonfunctional placeholders. The original-path file append, output save, gateway form submission, QR rendering and language UI use real application paths.',framesDirectory:work,assets:embeddedHashes,tooling:Object.fromEntries(['capture.cjs','fixture_test.go.txt','encode.py'].map(name=>[name,sha(fs.readFileSync(path.join(__dirname,name)))])),clips:records};
 fs.writeFileSync(path.join(output,'provenance.json'),JSON.stringify(report,null,2)+'\n');
 console.log('Tutorial frames retained for review at '+work);
})().catch(error=>{console.error(error);process.exitCode=1;});

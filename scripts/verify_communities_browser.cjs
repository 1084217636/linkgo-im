const assert=require('node:assert/strict');
const{chromium}=require('./browser/node_modules/playwright');
const fs=require('node:fs');
const {execFileSync}=require('node:child_process');
const base=process.env.LINKGO_COMMUNITY_WEB||'https://localhost:8443/community/';
const apiBase=process.env.LINKGO_API||'http://127.0.0.1:8090';
(async()=>{
 const browser=await chromium.launch({headless:true});const errors=[];
 const ac=await browser.newContext({ignoreHTTPSErrors:true}),bc=await browser.newContext({ignoreHTTPSErrors:true});
 const a=await ac.newPage(),b=await bc.newPage();const received=[];b.on('websocket',socket=>socket.on('framereceived',event=>received.push(Buffer.isBuffer(event.payload)?event.payload.toString():String(event.payload))));for(const p of[a,b])p.on('pageerror',e=>errors.push(e.message));
 const suffix=Date.now().toString(36),ga='浏览器社区 '+suffix,an='webA'+suffix,bn='webB'+suffix,pass='Browser-Test-2026!';
 async function login(page,name,register=true){await page.goto(base);await page.getByLabel('用户名',{exact:true}).fill(name);await page.getByLabel('密码',{exact:true}).fill(pass);if(register){await page.getByRole('button',{name:'注册账号',exact:true}).click();await page.getByRole('status').filter({hasText:'注册成功'}).waitFor()}await page.getByRole('button',{name:'登录',exact:true}).click();await page.getByRole('button',{name:'发现社区',exact:true}).waitFor()}
 async function api(path,token,method='GET',body){const r=await fetch(apiBase+'/api/v1'+path,{method,headers:{...(token?{Authorization:'Bearer '+token}:{}),...(body?{'Content-Type':'application/json'}:{})},body:body?JSON.stringify(body):undefined});assert.ok(r.ok,`${path}: ${r.status} ${await r.clone().text()}`);return r.json()}
 try{
 await login(a,an);await login(b,bn);
 await a.getByPlaceholder('社区名称',{exact:true}).fill(ga);await a.getByPlaceholder('社区简介',{exact:true}).fill('浏览器端到端验收');await a.getByRole('button',{name:'创建',exact:true}).click();await a.locator('.community-head h1').filter({hasText:ga}).waitFor();
 await b.getByRole('button',{name:'发现社区',exact:true}).click();await b.locator('button.group').filter({hasText:ga}).click();await b.locator('.community-head h1').filter({hasText:ga}).waitFor();
 console.log('PASS UI registration, login, create and join');
 const loginA=await api('/login',null,'POST',{username:an,password:pass}),loginB=await api('/login',null,'POST',{username:bn,password:pass});const groups=await api('/communities?mine=1',loginA.token);const gid=groups.communities.find(g=>g.name===ga).group_id;
 await a.getByLabel('帖子标题',{exact:true}).fill('浏览器中的学习笔记');await a.getByLabel('帖子正文',{exact:true}).fill('上传附件、成员评论和通知构成完整业务流程。');
 await a.getByLabel('帖子附件',{exact:true}).setInputFiles({name:'notes.txt',mimeType:'text/plain',buffer:Buffer.from('community attachment test')});await a.getByText('notes.txt',{exact:true}).waitFor();
 await a.getByRole('button',{name:'发布动态',exact:true}).click();await a.getByRole('button',{name:'浏览器中的学习笔记',exact:true}).waitFor();await a.getByRole('button',{name:'浏览器中的学习笔记',exact:true}).click();await a.getByRole('button',{name:'下载 notes.txt',exact:true}).waitFor();
 const attachments=await api('/posts/'+(await api(`/communities/${gid}/posts`,loginA.token)).posts[0].post_id,loginA.token);
 const fileResponse=await fetch(apiBase+'/api/v1/attachments/'+attachments.attachments[0].attachment_id+'/download',{headers:{Authorization:'Bearer '+loginB.token}});assert.equal(await fileResponse.text(),'community attachment test');
 await a.getByRole('button',{name:'精选',exact:true}).click();await a.getByRole('button',{name:'取消精选',exact:true}).waitFor();
 await b.getByRole('button',{name:'刷新动态',exact:true}).click();await b.getByRole('button',{name:'浏览器中的学习笔记',exact:true}).click();await b.getByLabel('评论内容',{exact:true}).fill('已阅读，准备完成学习任务');await b.getByRole('button',{name:'提交评论',exact:true}).click();await b.getByText('已阅读，准备完成学习任务',{exact:true}).waitFor();
 console.log('PASS UI post, object-store attachment, featured and comment');
 await a.getByRole('button',{name:'聊天',exact:true}).click();await b.getByRole('button',{name:'聊天',exact:true}).click();await a.getByRole('button',{name:'发送消息',exact:true}).waitFor();await a.getByLabel('聊天内容',{exact:true}).fill('跨浏览器实时讨论 '+suffix);await a.getByRole('button',{name:'发送消息',exact:true}).click();await b.locator('.bubble').filter({hasText:'跨浏览器实时讨论 '+suffix}).waitFor({timeout:60000});
 assert.match(await b.locator('.bubble').filter({hasText:'跨浏览器实时讨论 '+suffix}).locator('small').innerText(),new RegExp('^'+an+' · #'));
 console.log('PASS UI actual websocket group delivery and local persistence');
 await a.getByRole('button',{name:'成员',exact:true}).click();
 await a.locator('article').filter({hasText:bn}).getByRole('button',{name:'加为好友',exact:true}).click();
 await a.getByRole('status').filter({hasText:'好友申请已发送'}).waitFor();
 await b.getByRole('button',{name:/^好友私聊/}).click();
 await b.getByRole('button',{name:'刷新好友和申请',exact:true}).click();
 await b.locator('article').filter({hasText:loginA.user_id}).getByRole('button',{name:'接受',exact:true}).click();
 await a.getByRole('button',{name:/^好友私聊/}).click();
 await a.getByRole('button',{name:'刷新好友和申请',exact:true}).click();
 await a.getByRole('button',{name:bn,exact:true}).click();
 await b.getByRole('button',{name:an,exact:true}).click();
 await b.locator('button.group').filter({hasText:ga}).click();
 await b.getByRole('button',{name:'动态',exact:true}).click();
 await a.getByLabel('私聊内容',{exact:true}).fill('好友私聊 '+suffix);
 await a.getByRole('button',{name:'发送私聊',exact:true}).click();
 await b.getByRole('button',{name:/^好友私聊 1/}).waitFor({timeout:60000});
 await b.getByText('私聊 1',{exact:true}).waitFor();
 await b.getByRole('button',{name:/^好友私聊 1/}).click();
 await b.getByRole('button',{name:new RegExp('^'+an+' 1$')}).waitFor();
 await b.getByRole('button',{name:an,exact:false}).click();
 await b.locator('.bubble').filter({hasText:'好友私聊 '+suffix}).waitFor({timeout:60000});
 await b.getByRole('button',{name:'好友私聊',exact:true}).waitFor({timeout:60000});
 assert.equal(await b.getByText('私聊 0',{exact:true}).count(),0);
 await b.getByRole('button',{name:'退出登录',exact:true}).click();await login(b,bn,false);
 await b.getByRole('button',{name:/^好友私聊/}).click();
 await b.getByRole('button',{name:an,exact:true}).click();
 await b.locator('.bubble').filter({hasText:'好友私聊 '+suffix}).waitFor({timeout:60000});
 console.log('PASS UI friend request, acceptance, direct chat and history after login');
 await a.waitForTimeout(700);
 await a.locator('button.group').filter({hasText:ga}).click();
 await b.locator('button.group').filter({hasText:ga}).click();
 await a.locator('.community-head h1').filter({hasText:ga}).waitFor();
 await b.locator('.community-head h1').filter({hasText:ga}).waitFor();
 await a.getByRole('button',{name:'任务',exact:true}).click();await a.getByLabel('任务标题',{exact:true}).fill('浏览器学习任务');await a.getByLabel('任务说明',{exact:true}).fill('提交笔记，先退回再通过');await a.getByRole('button',{name:'创建任务草稿',exact:true}).click();await a.getByRole('button',{name:'发布任务',exact:true}).click();await a.getByText('任务状态：published',{exact:true}).waitFor();
 await b.getByRole('button',{name:'任务',exact:true}).click();await b.locator('button.task').filter({hasText:'浏览器学习任务'}).click();await b.getByLabel('任务提交内容',{exact:true}).fill('第一版成果');await b.getByRole('button',{name:'提交任务',exact:true}).click();await b.getByText('我的提交：submitted',{exact:true}).waitFor();
 await a.getByRole('button',{name:'刷新任务',exact:true}).click();await a.getByPlaceholder('退回理由',{exact:true}).fill('请补充例子');await a.getByRole('button',{name:'退回',exact:true}).click();await a.getByText('returned',{exact:false}).first().waitFor();
 await b.getByRole('button',{name:'刷新任务',exact:true}).click();await b.getByText('审核意见：请补充例子',{exact:true}).waitFor();await b.getByLabel('任务提交内容',{exact:true}).fill('第二版成果，包含例子');await b.getByRole('button',{name:'修改后重交',exact:true}).click();await b.getByText('我的提交：submitted',{exact:true}).waitFor();
 await a.getByRole('button',{name:'刷新任务',exact:true}).click();await a.getByRole('button',{name:'通过',exact:true}).click();await a.getByText('通过 1',{exact:false}).waitFor();await b.getByRole('button',{name:'刷新任务',exact:true}).click();await b.getByText('我的提交：approved',{exact:true}).waitFor();
 console.log('PASS UI full task publish, submit, return, resubmit, approve and progress');
 await b.getByRole('button',{name:/^通知 /}).click();await b.getByRole('button',{name:'刷新通知',exact:true}).click();await b.getByText('学习任务有更新',{exact:true}).first().waitFor({timeout:60000});
 await b.screenshot({path:'artifacts/community-notifications.png',fullPage:true});
 await a.getByRole('button',{name:'动态',exact:true}).click();await a.screenshot({path:'artifacts/community-feed.png',fullPage:true});
 await b.getByRole('button',{name:'退出登录',exact:true}).click();await login(b,bn,false);await b.locator('button.group').filter({hasText:ga}).click();await b.getByRole('button',{name:'任务',exact:true}).click();await b.locator('button.task').filter({hasText:'浏览器学习任务'}).click();await b.getByText('我的提交：approved',{exact:true}).waitFor();
 // Leave while keeping the same live socket; new group traffic must not reach B.
 const delayed='stale-recipient-'+suffix;
 execFileSync('docker',['pause','linkgo-next-transfer']);
 try {
 await a.getByRole('button',{name:'聊天',exact:true}).click();await a.getByLabel('聊天内容',{exact:true}).fill(delayed);await a.getByRole('button',{name:'发送消息',exact:true}).click();
 await a.locator('.bubble').filter({hasText:delayed}).waitFor();
 for(let i=0;i<40;i++){const h=await api('/history?target_id='+encodeURIComponent('group:'+gid)+'&limit=50',loginA.token);if((h.data||[]).some(m=>m.body===delayed))break;await new Promise(r=>setTimeout(r,250));if(i===39)throw Error('message was not persisted before leave')}
 await b.getByRole('button',{name:'成员',exact:true}).click();await b.getByRole('button',{name:'退出社区',exact:true}).click();await b.getByRole('heading',{name:'选择一个学习社区',exact:true}).waitFor();
 const revoked=await fetch(apiBase+'/api/v1/communities/'+gid+'/posts',{headers:{Authorization:'Bearer '+loginB.token}});assert.equal(revoked.status,404);
 }finally{execFileSync('docker',['unpause','linkgo-next-transfer'])}
 const history=await api('/history?target_id='+encodeURIComponent('group:'+gid)+'&limit=50',loginA.token);const delayedMessage=history.data.find(m=>m.body===delayed);assert.ok(delayedMessage);
 const deliveredKey='group_delivery:'+delayedMessage.message_id+':'+loginB.user_id;
 for(let i=0;i<40;i++){const state=execFileSync('docker',['exec','linkgo-next-redis','redis-cli','-a','123456','GET',deliveredKey],{stdio:['ignore','pipe','ignore']}).toString().trim();if(state==='done')break;await new Promise(r=>setTimeout(r,250));if(i===39)throw Error('queued job not processed')}
 await new Promise(r=>setTimeout(r,500));assert.ok(!received.some(frame=>frame.includes(delayed)),'former member received stale queued group payload');
 const afterLeave=await api('/login',null,'POST',{username:bn,password:pass});assert.ok(!(afterLeave.conversations||[]).some(c=>c.conversation_id==='group:'+gid),'stale login summary leaked departed community');
 console.log('PASS UI delayed Kafka recipient blocked at live websocket; stale login summaries filtered');
 assert.deepEqual(errors,[]);
 fs.writeFileSync('artifacts/community-browser-demo.json',JSON.stringify({community:ga,group_id:gid,admin:an,member:bn,password:pass},null,2));
 console.log('PASS UI persistent state after login, durable inbox, member leave; no page errors');
 }catch(e){console.error('admin:',await a.locator('body').innerText().catch(()=>''));console.error('member:',await b.locator('body').innerText().catch(()=>''));throw e}finally{await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});

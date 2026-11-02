#!/usr/bin/env python3
"""Create only dedicated demo accounts; never reset or overwrite existing data."""
import importlib.util,pathlib,uuid
spec=importlib.util.spec_from_file_location('checks',pathlib.Path(__file__).with_name('verify_learning_tasks.py'));m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
base='http://127.0.0.1:8090';password='Study-Demo-2026!'
def call(path,token=None,method='GET',body=None):return m.check(base,'/api/v1'+path,token,method,body)
def write(path,token,body=None):return call(path,token,'POST',{'request_id':'seed-'+uuid.uuid4().hex,**(body or{})})
users={}
for tag in('admin','member','guest'):
 name='demo_study_'+tag
 code,_=m.request(base,'/api/v1/register',method='POST',body={'username':name,'password':password})
 assert code in(200,409),'demo registration failed'
 users[tag]=call('/login',method='POST',body={'username':name,'password':password})
a,b,c=[users[x]['token']for x in('admin','member','guest')]
owned=call('/communities?mine=1',a)['communities']
def group(name,description):
 found=next((g for g in owned if g['name']==name),None)
 return found or write('/communities',a,{'name':name,'description':description})
go=group('Go 后端学习营','沉淀技术笔记，讨论实现细节，通过学习任务协作。')
ai=group('AI 论文读书会','围绕研究资料和论文展开学习；与 Go 社区内容隔离。')
write('/communities/'+go['group_id']+'/join',b);write('/communities/'+ai['group_id']+'/join',c)
for g,title,body in[(go,'欢迎来到 Go 后端学习营','先阅读公告，在聊天室打招呼，再打开学习任务提交一份笔记。'),(ai,'本周阅读安排','这个社区用于 AI 资料讨论，未加入的用户无法访问正文。')]:
 if not call('/communities/'+g['group_id']+'/posts',a)['posts']:
  write('/communities/'+g['group_id']+'/posts',a,{'kind':'announcement','title':title,'body':body})
tasks=call('/groups/'+go['group_id']+'/tasks',a)['tasks']
if not tasks:
 task=m.check(base,'/api/v1/groups/'+go['group_id']+'/tasks',a,'POST',{'request_id':'seed-'+uuid.uuid4().hex,'title':'用自己的话解释 Outbox','description':'提交一段文字，可附参考链接或文件。管理员可以退回并要求补充。','due_at':0},201)
 write('/tasks/'+task['task_id']+'/publish',a,{'expected_version':task['version']})
print('Community: https://localhost:8443/community/')
for tag in users:print(tag+': demo_study_'+tag+' / '+password)
print('Go community:',go['group_id']);print('AI community:',ai['group_id'])

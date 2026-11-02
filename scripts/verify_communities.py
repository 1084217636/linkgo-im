#!/usr/bin/env python3
"""Real API acceptance using freshly registered users and isolated communities.
Pass --faults to pause this demo stack's Kafka and verify recovery. No data reset.
"""
import argparse, concurrent.futures, importlib.util, json, pathlib, subprocess, time, uuid
spec=importlib.util.spec_from_file_location('task_checks',pathlib.Path(__file__).with_name('verify_learning_tasks.py'))
checks=importlib.util.module_from_spec(spec);spec.loader.exec_module(checks)
rid=lambda:uuid.uuid4().hex

def main():
 p=argparse.ArgumentParser();p.add_argument('--base',default='http://127.0.0.1:8090');p.add_argument('--faults',action='store_true');args=p.parse_args();base=args.base
 def call(path,token=None,method='GET',body=None,status=200):
  return checks.check(base,'/api/v1'+path,token,method,body,status)
 def write(path,token,body=None,method='POST',status=200):return call(path,token,method,{'request_id':rid(),**(body or {})},status)
 suffix=rid()[:10];users={}
 for tag in 'ABC':
  username='community'+tag+suffix;result=call('/register',method='POST',body={'username':username,'password':'Test-Pass-2026!'})
  users[tag]={**call('/login',method='POST',body={'username':username,'password':'Test-Pass-2026!'}),'username':username}
 a,b,c=[users[t]['token'] for t in 'ABC'];au,bu,cu=[users[t]['user_id'] for t in 'ABC']
 call('/register',method='POST',body={'username':users['A']['username'],'password':'Test-Pass-2026!'},status=409)
 call('/register',method='POST',body={'username':'x','password':'123'},status=400)
 g=write('/communities',a,{'name':'Go学习社区 '+suffix,'description':'接口与浏览器联合验收'})['group_id']
 g2=write('/communities',a,{'name':'AI学习社区 '+suffix})['group_id']
 write(f'/communities/{g}/join',b);write(f'/communities/{g}/join',b);write(f'/communities/{g2}/join',c)
 assert len([x for x in call('/communities?mine=1',a)['communities'] if x['group_id'] in (g,g2)])==2
 call(f'/communities/{g2}/posts',b,status=404)
 print('PASS registration, duplicate username, create/join, multi-community isolation')
 body={'request_id':rid(),'kind':'note','title':'事务和重试 '+suffix,'body':'学习笔记：事务提交不等于客户端收到。'}
 def duplicate(_):return checks.request(base,f'/api/v1/communities/{g}/posts',b,'POST',body)
 with concurrent.futures.ThreadPoolExecutor(max_workers=2)as pool:results=list(pool.map(duplicate,range(2)))
 assert [x[0] for x in results]==[200,200],results
 post=results[0][1]['post_id'];assert post==results[1][1]['post_id']
 call(f'/communities/{g}/posts',b,'POST',{**body,'title':'wrong payload'},409)
 call(f'/communities/{g}/posts',b,'POST',{'request_id':rid(),'kind':'announcement','title':'非法公告','body':'越权'},404)
 edit={'title':body['title'],'body':body['body']+' 补充内容','expected_version':1}
 def edit_race(_):return checks.request(base,f'/api/v1/posts/{post}',b,'PATCH',{'request_id':rid(),**edit})
 with concurrent.futures.ThreadPoolExecutor(max_workers=2)as pool:race=list(pool.map(edit_race,range(2)))
 assert sorted(x[0] for x in race)==[200,409],race
 call('/posts/'+post,c,status=404)
 call('/posts/'+post+'/like',a,'PUT');call('/posts/'+post+'/like',a,'PUT');assert call('/posts/'+post,a)['likes']==1
 comment=write('/posts/'+post+'/comments',a,{'body':'很有帮助，补充一个例子？'})
 write('/posts/'+post+'/comments',b,{'body':'好的，下面补充','parent_id':comment['comment_id']})
 nested=call('/posts/'+post+'/comments',b)['comments'][-1]['comment_id']
 write('/posts/'+post+'/comments',a,{'body':'不能无限嵌套','parent_id':nested},status=400)
 assert len(call('/posts/'+post+'/comments',a)['comments'])==2
 print('PASS post idempotency, request payload conflict, concurrent edit, comments and reaction uniqueness')
 # Stable feed keyset pages, including equal-millisecond timestamps.
 for i in range(22):write(f'/communities/{g}/posts',b,{'kind':'note','title':f'分页 {i}','body':'分页验证'})
 page=call(f'/communities/{g}/posts',a);assert len(page['posts'])==20 and page['next_cursor']
 page2=call(f'/communities/{g}/posts?cursor='+page['next_cursor'],a)
 assert not {x['post_id'] for x in page['posts']} & {x['post_id'] for x in page2['posts']}
 print('PASS keyset feed pagination')
 task=write(f'/groups/{g}/tasks',a,{'title':'阅读事务与Outbox','description':'提交笔记','due_at':0},status=201)
 tid=task['task_id'];write(f'/tasks/{tid}/publish',a,{'expected_version':1})
 sub=write(f'/tasks/{tid}/submission',b,{'text':'第一版','expected_version':0},'PUT')
 sid=sub['submission_id'];ret=write(f'/submissions/{sid}/review',a,{'expected_version':sub['version'],'decision':'return','reason':'请补充失败场景'})
 revised=write(f'/tasks/{tid}/submission',b,{'text':'第二版：Kafka不可用时从Outbox恢复','expected_version':ret['version']},'PUT')
 approved=write(f'/submissions/{sid}/review',a,{'expected_version':revised['version'],'decision':'approve'})
 assert approved['status']=='approved' and call(f'/tasks/{tid}/progress',a)['approved']==1
 call(f'/tasks/{tid}',c,status=404)
 print('PASS task publish → submit → return → resubmit → approve, permissions and progress')
 def wait_notification(token,object_id,kind=None,seconds=45,count=1):
  end=time.monotonic()+seconds
  while time.monotonic()<end:
   result=call('/notifications',token)['notifications']
   found=[x for x in result if x['object_id']==object_id and(not kind or x['kind']==kind)]
   if len(found)>=count:return found
   time.sleep(.5)
  raise AssertionError('notification did not recover '+object_id)
 found=wait_notification(b,post,'comment_created');call(f"/notifications/{found[0]['id']}/read",b,'POST')
 assert next(x for x in call('/notifications',b)['notifications'] if x['id']==found[0]['id'])['read_at']>0
 assert len(wait_notification(b,tid,'task_review',count=2))==2
 print('PASS Kafka community projection, task-event reuse and persistent notification read state')
 if args.faults:
  subprocess.run(['docker','stop','linkgo-next-kafka'],check=True,capture_output=True)
  try:
   offline=write(f'/communities/{g}/posts',a,{'kind':'announcement','title':'Kafka中断后仍能发布','body':'MySQL事务和外部通知解耦'})['post_id']
   assert call('/posts/'+offline,b)['title']=='Kafka中断后仍能发布'
  finally:subprocess.run(['docker','start','linkgo-next-kafka'],check=True,capture_output=True)
  wait_notification(b,offline,seconds=75)
  print('PASS Kafka outage: committed post survives and outbox notification recovers')
 write(f'/communities/{g}/leave',b)
 for path in (f'/posts/{post}',f'/posts/{post}/comments',f'/tasks/{tid}',f'/communities/{g}/posts'):
  call(path,b,status=404)
 assert not [n for n in call('/notifications',b)['notifications'] if n['group_id']==g]
 write(f'/communities/{g}/leave',a,status=409)
 write(f'/communities/{g}/join',b);write(f'/communities/{g}/transfer',a,{'user_id':bu});write(f'/communities/{g}/leave',a)
 print('PASS leave revokes reads, notifications filtered, owner transfer before leaving')
 # Store only demo credentials, never model keys or production credentials.
 evidence={'group_id':g,'second_group_id':g2,'post_id':post,'task_id':tid,'users':{k:{'username':v['username'],'user_id':v['user_id']}for k,v in users.items()},'password':'Test-Pass-2026!'}
 target=pathlib.Path('artifacts/community-demo.json');target.parent.mkdir(exist_ok=True);target.write_text(json.dumps(evidence,ensure_ascii=False,indent=2))
 print('PASS complete; reproducible fixture:',target)
if __name__=='__main__':main()

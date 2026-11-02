#!/usr/bin/env python3
"""Pause this demo Transfer, observe real Prometheus→Alertmanager→local webhook,
then unpause and verify resolved delivery. No external message is sent.
"""
import json,subprocess,time,urllib.parse,urllib.request

def get(url):
 with urllib.request.urlopen(url,timeout=5)as r:return json.load(r)
def metric(state):
 q='sum(linkgo_alert_webhooks_total{state="'+state+'"})'
 result=get('http://localhost:9090/api/v1/query?'+urllib.parse.urlencode({'query':q}))['data']['result']
 return float(result[0]['value'][1])if result else 0

def wait(fn,timeout=90):
 until=time.monotonic()+timeout
 while time.monotonic()<until:
  try:
   if fn():return
  except Exception:pass
  time.sleep(2)
 raise AssertionError('observation did not arrive')

firing=metric('firing');resolved=metric('resolved')
subprocess.run(['docker','pause','linkgo-next-transfer'],check=True,capture_output=True)
try:
 wait(lambda:metric('firing')>firing)
 print('PASS real target outage → alert rule firing → Alertmanager → authenticated local receiver')
finally:subprocess.run(['docker','unpause','linkgo-next-transfer'],check=True,capture_output=True)
wait(lambda:metric('resolved')>resolved)
print('PASS recovered target → resolved webhook')
query='{project="linkgo",service="gateway-a"} |= "local alert received"'
logs=get('http://localhost:3100/loki/api/v1/query_range?'+urllib.parse.urlencode({'query':query,'limit':20}))
assert logs['data']['result'],'alert audit logs did not reach Loki'
print('PASS Docker logs → Alloy → Loki; Grafana datasource provisioned')

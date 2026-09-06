from pathlib import Path
import hashlib,json,re,subprocess,sys
root=Path.cwd()
u=Path('docs/target/yunka-upgrade')
m=json.loads((u/'FINAL-MANIFEST.json').read_text())
def git(*args): return subprocess.check_output(['git',*args],text=True).strip()
def digest(p): return hashlib.sha256(Path(p).read_bytes()).hexdigest()
head=git('rev-parse','HEAD')
if len(sys.argv)>1: assert head==sys.argv[1], (head,sys.argv[1])
parent=m['fixed_parent']; allowed=set(m['allowed_changed_paths'])
assert parent=='1da771dac46c1b10c2ea54a0fb4559316c20179b'
assert m['source_tree']==git('rev-parse',parent+'^{tree}')
assert m['framework_sha']==git('ls-tree','HEAD','third_party/yunka').split()[2]
ops=json.loads(Path('backend-yunka/contracts/generated/operation-plans.json').read_text())['operations']
assert m['generated_delivery_operation_count']==len(ops)==25
assert m['generated_delivery_operation_ids']==[o['operationId'] for o in ops]
assert len(set(m['generated_delivery_operation_ids']))==25
checks=[]
for group in ['source_file_inventory','historical_evidence_inventory','current_document_inventory']:
 for row in m[group]:
  assert Path(row['path']).is_file(),row
  assert digest(row['path'])==row['sha256'], row['path']
 checks.append({'check':group,'files':len(m[group]),'result':'pass'})
assert set(x['path'] for x in m['current_document_inventory'])==allowed-{str(u/'FINAL-MANIFEST.json')}
assert len(m['framework_issue_readback'])==3
assert all(i['state']=='closed' and not i['fix_adopted_by_yu33'] for i in m['framework_issue_readback'])
rows=subprocess.check_output(['git','ls-tree','-r','HEAD'],text=True).splitlines()
protected=[r for r in rows if r.split('\t',1)[1] not in allowed]
assert hashlib.sha256(('\n'.join(protected)+'\n').encode()).hexdigest()==m['unchanged_git_objects_sha256']
changed=set(git('diff','--name-only',parent).splitlines())
unknown=set(git('ls-files','--others','--exclude-standard').splitlines())
assert changed|unknown <= allowed, sorted((changed|unknown)-allowed)
assert not git('diff','--name-only',parent,'--','third_party','backend','web')
references=[];syntax=[]
for p in sorted(allowed):
 if not p.endswith('.md'):continue
 text=Path(p).read_text()
 for link in re.findall(r'\]\(([^)]+)\)',text):
  if re.match(r'^[a-zA-Z][\w+.-]*:',link) or link.startswith('#'):continue
  target=(Path(p).parent/link.split('#',1)[0]).resolve()
  assert target.is_relative_to(root),link
  assert target.is_file(), (p,link)
  references.append({'source':p,'target':str(target.relative_to(root))})
 for block in re.findall(r'```bash\n(.*?)```',text,re.S):
  subprocess.run(['bash','-n'],input=block,text=True,check=True,capture_output=True)
  syntax.append(p)
# Document RED is checked against actual immutable parent, not missing tools.
old_backend=git('show',parent+':backend-yunka/README.md')
old_migration=git('show',parent+':docs/YUNKA-MVP-MIGRATION.md')
old_arch=git('show',parent+':docs/ARCHITECTURE.md')
old_source=git('show',parent+':docs/SOURCE-MANIFEST.md')
red={
 'operation_count_19_vs_25':'当前声明 19 个 operation plan' in old_backend,
 'operation_count_13_vs_25':'13 个 operation plan' in old_migration,
 'mandatory_legacy_key':'IOT_DELIVERY_LOCAL_API_KEY` 是必填' in old_backend,
 'members_still_unimplemented':'成员管理尚未实现' in old_migration,
 'legacy_db_as_current':'backend/data/iot-delivery.db' in old_arch,
 'old_framework_as_current':'9a51562aa7bcef42f6861bd91abd30aae13ed6ef' in old_source}
assert all(red.values()),red
now_backend=Path('backend-yunka/README.md').read_text()
assert '25 个生成 operation plan' in now_backend
assert 'IOT_DELIVERY_LOCAL_API_KEY` 是必填' not in now_backend
assert '成员管理尚未实现' not in Path('docs/YUNKA-MVP-MIGRATION.md').read_text()
assert '## 历史记录' in Path('docs/SOURCE-MANIFEST.md').read_text()
assert '15 个 Unicode 码点' in now_backend and '4096 字节' in now_backend
assert '## 调度边界' in (u/'TASKS.md').read_text()
assert 'YU-33 尚未启动' not in (u/'TASKS.md').read_text()
subprocess.run(['git','diff','--check'],check=True)
result={'task_id':'YU-33','head':head,'fixed_parent':parent,'framework_sha':m['framework_sha'],'result':'pass','baseline_document_red':red,'inventory_checks':checks,'checked_local_links':len(references),'bash_syntax_blocks':len(syntax),'scope':'documentation only; runtime execution belongs to exact-SHA CI','changed_paths':sorted(changed|unknown),'manifest_sha256':digest(u/'FINAL-MANIFEST.json'),'protected_git_objects_unchanged':True}
print(json.dumps(result,ensure_ascii=False,indent=2))

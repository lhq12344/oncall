# OnCall 涓绘枃妗ｏ紙褰撳墠瀹炵幇涓€鑷寸増锛?

> 鏇存柊鏃堕棿锛?026-03-22
> 鏍″噯鑼冨洿锛歚main.go`銆乣api/chat/v1/chat.go`銆乣internal/controller/chat/chat_v1.go`銆乣internal/agent/*`銆乣internal/context/*`銆乣utility/mem/*`

## 1. 鏂囨。瀹氫綅

- 杩欐槸 `oncall/docs` 涓嬬殑涓绘枃妗ｏ紙source of truth锛夈€?
- `闈㈣瘯浜偣.md`銆乣闈㈣瘯搴旂瓟鎸囧崡.md`銆乣椤圭洰浠嬬粛.md`銆乣interview-analysis.md` 浣滀负琛ュ厖鏉愭枡淇濈暀銆?
- 濡傝ˉ鍏呮枃妗ｄ笌瀹炵幇鍐茬獊锛屼互鏈枃涓哄噯銆?

## 2. 涓€鍙ヨ瘽浠嬬粛

OnCall 鏄竴涓熀浜?GoFrame + Eino ADK 鐨勫 Agent 杩愮淮绯荤粺锛氱敤鑷劧璇█鍙戣捣鏁呴殰澶勭悊锛屽畬鎴愯娴嬨€丷CA銆佷慨澶嶆墽琛岋紙鍚鎵逛腑鏂?鎭㈠锛夈€佹姤鍛婁骇鍑哄拰鐭ヨ瘑娌夋穩銆?

## 3. 褰撳墠鏋舵瀯锛堝弻杞級

```text
Frontend (React + Vite + TypeScript + Zustand)
        鈹?
        鈻?
HTTP/SSE API (/api/v1/*, port 6872)
        鈹?
        鈻?
Controller(chat_v1) + ADK Runner
   鈹溾攢 杞ㄩ亾 A: dialogue_agent锛堣亰澶?宸ュ叿缂栨帓锛?
   鈹斺攢 杞ㄩ亾 B: incident_workflow_agent锛堟晠闅滃缃伐浣滄祦锛?
```

### 3.1 杞ㄩ亾 A锛氬璇濊建

- 鍏ュ彛锛歚POST /api/v1/chat_stream`
- Runner锛歚chatStreamRunner`
- 涓昏鑳藉姏锛氭剰鍥捐瘑鍒€佺煡璇嗘绱€並8s/鎸囨爣鏌ヨ銆佸彈鎺?Bash 鎵ц銆佸閮ㄦ绱?
- 涓柇鎭㈠锛歚POST /api/v1/chat_resume_stream`

### 3.2 杞ㄩ亾 B锛氳繍缁村伐浣滄祦杞?

- 鍏ュ彛锛歚POST /api/v1/ai_ops_stream`
- Runner锛歚opsStreamRunner`
- 宸ヤ綔娴佺粨鏋勶紙婧愮爜鐪熷疄锛夛細

```text
Sequential(
  Loop(
    incident_analysis,
    diagnosis_gate,
    plan,
    plan_gate,
    plan_approval,
    execute_plan,
    verify_plan,
    replan_decider
  ),
  final_report
)
```

- Loop 鏈€澶ц疆娆★細`MaxExecutionLoops`锛岄粯璁?`3`
- 鎭㈠鎺ュ彛锛歚POST /api/v1/ai_ops_resume_stream`

## 4. 瀵瑰鎺ュ彛涓庢祦寮忓崗璁?

### 4.1 HTTP 鎺ュ彛锛堝綋鍓嶅疄鐜帮級

| 鏂规硶 | 璺緞 | 璇存槑 |
|---|---|---|
| POST | `/api/v1/chat_stream` | 鏅€氬璇濇祦寮忚緭鍑?|
| POST | `/api/v1/chat_resume_stream` | 瀵硅瘽涓柇鎭㈠ |
| POST | `/api/v1/ai_ops_stream` | 杩愮淮宸ヤ綔娴佹祦寮忚緭鍑?|
| POST | `/api/v1/ai_ops_resume_stream` | 杩愮淮涓柇鎭㈠ |
| POST | `/api/v1/upload` | 鐭ヨ瘑鏂囦欢涓婁紶锛坄multipart/form-data`锛?|
| GET | `/api/v1/monitoring` | 鐩戞帶鍗犱綅鎺ュ彛锛堝綋鍓嶈繑鍥為粯璁ゅ€硷級 |

### 4.2 SSE 浜嬩欢璇箟

- `chat_stream`锛?
  - 鏅€氬唴瀹癸細鐩存帴鏂囨湰 chunk锛坄data: <text>`锛?
  - 涓柇锛欽SON锛坄type=interrupt`锛?
  - 缁撴潫锛歚[DONE]`
  - 閿欒锛歚[ERROR] ...`
- `ai_ops_stream` / `ai_ops_resume_stream`锛?
  - 姝ラ锛歚{"type":"step","step":n,"content":"..."}`
  - 鍐呭锛歚{"type":"content","content":"..."}`
  - 涓柇锛歚{"type":"interrupt", ...}`
  - 缁撴潫锛歚{"type":"done"}`
  - 閿欒锛歚{"type":"error","content":"..."}`

### 4.3 涓柇鎭㈠璇锋眰鍏抽敭瀛楁

- `checkpoint_id`锛氬畾浣嶄竴娆″彲鎭㈠鎵ц瀹炰緥
- `interrupt_ids[]`锛氬畾浣嶅叿浣撳緟鎭㈠涓柇鐐癸紙鍙┖锛岀┖鍒?checkpoint 绾ф仮澶嶏級
- `approved/resolved/comment/selection_value`锛氭仮澶嶅喅绛栬浇鑽?

## 5. Agent 鍒嗗眰鑱岃矗

| Agent | 鑱岃矗 | 鏍稿績宸ュ叿/杈撳嚭 |
|---|---|---|
| `dialogue_agent` | 瀵硅瘽鍏ュ彛銆佸伐鍏风紪鎺?| `intent_analysis`銆乣request_detail_selection`銆乣knowledge_retrieve`銆乣ops_case_retrieve`銆乣k8s_monitor`銆乣metrics_collector`銆乣web_search`銆乣bash_execute_with_approval` |
| `knowledge_agent` | 鏂囨湰鐭ヨ瘑涓婁紶銆佸垎鐗囩储寮?| 涓婁紶閾捐矾 `BuildKnowledgeUploadChain` |
| `incident_analysis` | 瑙傛祴銆丷CA 涓庝慨澶嶆剰鍥惧垎鏋?| 杈撳嚭 Diagnosis / RemediationProposal锛屽啓鍏?`IncidentState` |
| `diagnosis_gate` | 璇婃柇璇佹嵁涓庝慨澶嶆剰鍥鹃棬鎺?| 鏍￠獙璇佹嵁銆佹牴鍥犮€佸奖鍝嶉潰銆乫allback 绛夎繘鍏ヨ鍒掑墠鏉′欢 |
| `plan` | 鐢熸垚 canonical ExecutionPlan | `normalize_plan -> generate_plan`锛屽啓鍏?`PlanState` |
| `plan_gate` | 鏍￠獙 canonical ExecutionPlan | `validate_plan`锛屾鏌ラ闄┿€佸洖婊氥€佹垚鍔熸爣鍑嗕笌瀹℃壒杈圭晫 |
| `plan_approval` | 鏁翠綋璁″垝瀹℃壒缁戝畾 | 缁戝畾 `plan_id + plan_revision + approval_snapshot_hash` |
| `execute_plan` | 浠呮墽琛屽凡鎵瑰噯璁″垝 | `execute_step -> validate_result -> rollback`锛涗笉鐢熸垚鎴栨敼鍐欒鍒?|
| `verify_plan` | 鍏ㄨ鍒掓墽琛岀粨鏋滈獙璇?| 鏍￠獙 executed_steps 瑕嗙洊鐜囧拰 canonical success criteria |
| `replan_decider` | 閲嶈鍒掑喅绛栦笌寰幆鏀舵暃 | 杈撳嚭 complete / refresh_observation / manual_required / abort |
| `final_report` | 鏈€缁堟€荤粨杈撳嚭涓庢姤鍛婅惤鐩?| 姹囨€?`IncidentState`銆丳lanState銆丷eplanState 鐢熸垚鏈€缁堟姤鍛?|

## 6. 鐘舵€佹ā鍨嬩笌鎭㈠鏈哄埗

### 6.1 Session Memory锛堝璇濊蹇嗭級

- 瀹炵幇锛?
  - 涓婂眰锛歚internal/context/session_memory.go`
  - 搴曞眰锛歚utility/mem/mem.go`
- 瀛樺偍锛歊edis锛坱urns + summary + meta + sys锛?
- 鐩爣锛氭帶鍒朵笂涓嬫枃 token锛岄伩鍏嶉暱鏃ュ織姹℃煋杈撳叆
- 鍘嬬缉绛栫暐锛堝綋鍓嶅疄鐜帮級锛?
  - 瑙﹀彂闃堝€硷細鍘嗗彶杞 > 40
  - 姣忔鍘嬬缉锛氭渶鏃?20 杞?
  - 鎽樿鏂瑰紡锛氳鍒欐嫾鎺ワ紙`- 鐢ㄦ埛: ... / - 鍔╂墜: ...`锛夛紝涓嶆槸 LLM 鎽樿
  - 鎽樿闀垮害锛氶粯璁?1200 runes
- 琛ュ厖璇存槑锛歚dialogue_agent` 杩樻寕浜?Eino summarization middleware锛屼絾鏁呴殰宸ヤ綔娴佷富閾剧殑涓婁笅鏂囨帶鍒舵牳蹇冧粛鐒舵槸 `Graph State + HistoryRewriter`

### 6.2 Checkpoint Store锛堟墽琛屾鏌ョ偣锛?

- 瀹炵幇锛歚internal/context/checkpoint_store.go`
- Redis Key锛歚oncall:checkpoint:<checkpoint_id>`
- Value锛欰DK checkpoint bytes
- TTL锛氶粯璁?24h
- 鐢ㄩ€旓細`Runner.Resume/ResumeWithParams` 鎭㈠鎵ц鍥?

### 6.3 Graph State锛堝伐浣滄祦鐘舵€侊級

- 绫诲瀷锛歚IncidentState`锛坄internal/workflow/ops/state_bridge.go`锛?
- 閫氳繃 session values 缁存姢锛屽瓧娈佃鐩?observation/rca/proposal/execution/final status
- `incidentHistoryRewriter` 鍦ㄦ瘡杞ā鍨嬭緭鍏ュ墠鍙敞鍏ワ細
  - 鏈€鏂扮敤鎴疯緭鍏?
  - 瑁佸壀鍚庣殑 Graph State锛圝SON锛?

### 6.4 Execution Tool State锛堟墽琛屽唴鐘舵€侊級

- Key锛歚_execution_tool_state_v1`
- 缁撴瀯锛歚executionToolState`锛坓ob 缂栬В鐮侊級
- 浣滅敤锛氳褰曡鍒掑噯澶囥€佹楠ゆ墽琛屻€侀獙璇佺姸鎬併€侀噸澶嶅け璐ヨ鏁帮紝淇濊瘉 checkpoint 鎭㈠鍚庢墽琛岀姸鎬佽繛缁?

## 7. 鎵ц瀹夊叏鏈哄埗

### 7.1 闈欐€佽鍒掗闄╂牎楠岋紙`validate_plan`锛?

- 缁濆绂佹锛坆locked锛夛細`rm -rf /`銆乣mkfs`銆乣dd if=`銆乣shutdown/reboot`銆乫ork bomb
- 楂橀闄╋紙瀹￠槄/纭锛夛細`kubectl delete/drain/scale/patch/...`銆乣docker stop/restart/rm`銆乣systemctl stop/restart/disable`銆乣helm upgrade/rollback/uninstall`
- 鍙鍛戒护璇嗗埆锛歚kubectl get/describe/logs/top`銆乣cat/ls/ps/...`

### 7.2 杩愯鏃跺鎵逛笌鎵ц锛坄execute_step`锛?

- 鐧藉悕鍗曞懡浠ら泦锛坆ash/kubectl/docker/systemctl/curl 绛夛級
- 鍙樻洿绫绘楠ゆ墽琛屽墠瑙﹀彂 `tool.Interrupt(...)`
- 鎭㈠鏃堕€氳繃 `tool.GetResumeContext(...)` 璇诲彇 `approved/resolved/comment` 鍐崇瓥
- 鍛戒护鎵ц鏈?timeout锛岃緭鍑烘湁瑁佸壀锛岄槻姝㈤暱鏃ュ織澶辨帶

### 7.3 寰幆鏀舵暃涓庣啍鏂?

- 澶栧眰锛歚verify_plan` 涓?`replan_decider` 鎸夋墽琛?楠岃瘉浜嬪疄鍐冲畾瀹屾垚銆侀噸鏂拌娴嬨€佽浆浜哄伐鎴栫粓姝?- 閲嶅闂涓婇檺锛氶粯璁?`3` 娆″悓绫诲け璐ュ悗鍋滄鑷姩閲嶈瘯骞惰浆浜哄伐
- 鍐呭眰锛歟xecution tool state 瀵瑰悓涓€姝ラ閲嶅澶辫触鏈夐澶栭槇鍊间繚鎶?

## 8. 鍓嶇瀹炵幇鍙ｅ緞锛堝綋鍓嶏級

- 鎶€鏈爤锛歊eact 19 + TypeScript + Vite 6 + Zustand
- 鍏抽敭鏂囦欢锛?
  - `frontend/src/services/api.ts`锛圫SE 瀹㈡埛绔笌浜嬩欢瑙ｆ瀽锛?
  - `frontend/src/store/useStore.ts`锛堝叏灞€鐘舵€佷笌鎸佷箙鍖栵級
  - `frontend/src/components/InterruptCard.tsx`锛堜腑鏂鎵?UI锛?
- 鍓嶇瀵?SSE 閲囩敤鈥淛SON 浼樺厛 + 鏂囨湰鍥為€€鈥濊В鏋愮瓥鐣?

## 9. 杩愯涓庝緷璧?

- 杩涚▼鍏ュ彛锛歚main.go`
- 鐩戝惉绔彛锛歚6872`锛坄main.go` 鏄惧紡 `SetPort(6872)`锛?
- 鍏抽敭渚濊禆锛?
  - Redis锛氫細璇濊蹇?+ checkpoint
  - MySQL锛氫笟鍔℃寔涔呭寲鍒濆鍖?
  - Elasticsearch锛氬彲閫夛紝澶辫触鏃堕檷绾?
  - Prometheus/K8s锛氳繍缁村伐鍏锋煡璇?
  - Milvus锛氱煡璇嗘绱?妗堜緥妫€绱?
- K8s 娓呭崟璇存槑锛氭湰浠撳簱 `manifest/k8s/README.md` 宸插０鏄庣粺涓€娓呭崟浣嶄簬 `/home/lihaoqian/project/k8s`

## 10. 宸叉牎鍑嗗彛寰勶紙閬垮厤鏃ф枃妗ｅ啿绐侊級

1. 鍓嶇涓嶆槸鈥淰anilla JS 椤甸潰鈥濓紝鏄?React + TS + Zustand銆?
2. 褰撳墠 API 涓嶆槸 `/api/v1/chat` / `/api/v1/chat_resume`锛岃€屾槸 `*_stream` 璺緞銆?
3. 浼氳瘽鍘嬬缉鎽樿鏄鍒欐嫾鎺ワ紝涓嶆槸 LLM 鎽樿鐢熸垚銆?
4. Incident Loop 涓嶆槸鏃犻檺寰幆锛岄粯璁ゆ渶澶?3 杞€?
5. Checkpoint 鏄?Redis bytes 瀛樺偍瀹炵幇锛屾仮澶嶇敱 ADK Runner 椹卞姩銆?
6. SSE 鍗忚鏄€滄枃鏈?JSON 娣峰悎鈥濓紝涓嶆槸鍗曚竴 JSON 娴併€?
7. `/api/v1/upload` 鏄煡璇嗕笂浼犻摼璺紝涓嶆槸瀵硅薄瀛樺偍鏂囦欢鏈嶅姟銆?

## 11. 闈㈣瘯閫熻妯℃澘

### 11.1 30 绉掔増

OnCall 鏄竴涓 Agent 杩愮淮绯荤粺锛屽悗绔敤 GoFrame + Eino ADK銆傚畠鎶婃晠闅滃鐞嗘媶鎴愯瘖鏂€佽鍒掋€佸鎵广€佹墽琛屻€侀獙璇併€侀噸瑙勫垝鍜屾渶缁堟姤鍛婏紝骞剁敤 SSE 瀹炴椂杈撳嚭銆傞珮椋庨櫓璁″垝鍏堢粡杩?`plan_gate` / `plan_approval`锛屽懡浠ょ骇鍙樻洿鍦?`execute_step` 缁х画涓柇绛変汉宸ュ鎵癸紝瀹℃壒鍚庣敤 `checkpoint_id + interrupt_ids` 浠庢柇鐐规仮澶嶃€傜姸鎬佷笂鎶婁細璇濊蹇嗐€丟raph State銆丆heckpoint 鍒嗗眰锛屾棦鑳芥帶 token锛屼篃鑳戒繚璇侀暱娴佺▼鍙仮澶嶃€?
### 11.2 2 鍒嗛挓鐗?

椤圭洰鏈変袱鏉′富閾捐矾锛氫竴鏉℃槸 `chat_stream` 鐨勫璇濋摼锛屽仛鎰忓浘璇嗗埆銆佺煡璇嗘绱㈠拰杞婚噺杩愮淮宸ュ叿璋冪敤锛涘彟涓€鏉℃槸 `ai_ops_stream` 鐨勬晠闅滃缃摼銆傛晠闅滈摼鏄?`Sequential + Loop`锛歚incident_analysis` 缁熶竴瀹屾垚瑙傛祴/RCA/淇鎰忓浘锛宍diagnosis_gate` 鍐冲畾鏄惁鑳借繘鍏ヨ鍒掞紝`plan` 浜у嚭 canonical ExecutionPlan锛宍plan_gate` 涓?`plan_approval` 缁戝畾鏁翠唤璁″垝锛宍execute_plan` 鍙秷璐瑰凡鎵瑰噯璁″垝锛宍verify_plan` 鍋氬叏璁″垝楠屾敹锛宍replan_decider` 鍐冲畾瀹屾垚銆侀噸鏂拌娴嬨€佽浆浜哄伐鎴栫粓姝紝鏈€鍚?`final_report` 姹囨€昏惤鐩樸€?瀹夊叏涓婃槸涓ゅ眰鍔犱竴鏉″洖璺細`plan_gate` 鍋氳鍒掔骇瀹屾暣鎬?椋庨櫓/鍥炴粴绛涙煡锛宍execute_step` 瀵瑰彉鏇村懡浠ら€愭瀹℃壒骞跺彲鎭㈠鎵ц锛宍replan_decider` 鎶婂け璐ョ粺涓€鏀舵暃鎴愮粨鏋勫寲 ReplanDecision銆傜姸鎬佸眰鍒嗕笁鍧楋細SessionMemory 绠?token銆両ncidentState/PlanState/ReplanState 绠℃祦绋嬭涔夈€丆heckpoint 绠″彲鎭㈠鎵ц銆傝繖鏍锋棦淇濊瘉鍙搷浣滄€э紝涔熶繚璇佺敓浜у畨鍏ㄨ竟鐣屻€?
## 12. 鍏抽敭浠ｇ爜绱㈠紩

- 鏈嶅姟鍏ュ彛锛歚main.go`
- 璺敱濂戠害锛歚api/chat/v1/chat.go`
- 鎺у埗鍣ㄤ笌 SSE锛歚internal/controller/chat/chat_v1.go`
- 搴旂敤瑁呴厤锛歚internal/bootstrap/app.go`
- 宸ヤ綔娴佺紪鎺掞細`internal/workflow/ops/incident_workflow.go`
- Gate 涓庢渶缁堟姤鍛婏細`internal/workflow/ops/incident_nodes.go`
- Graph State 涓庡巻鍙查噸鍐欙細`internal/workflow/ops/state_bridge.go`
- Execution Agent锛歚internal/execution/agent.go`
- 璁″垝鏍￠獙锛歚internal/execution/tools/validate_plan.go`
- 姝ラ鎵ц涓庡鎵癸細`internal/execution/tools/execute_step.go`
- 鎵ц鍐呯姸鎬侊細`internal/execution/tools/tool_call_state.go`
- Session Memory锛歚internal/context/session_memory.go`
- Redis Checkpoint锛歚internal/context/checkpoint_store.go`
- 璁板繂鍘嬬缉瀹炵幇锛歚utility/mem/mem.go`
- 鍓嶇 API锛歚frontend/src/services/api.ts`
- 鍓嶇鐘舵€侊細`frontend/src/store/useStore.ts`



# OnCall Ops Workflow 褰撳墠瀹屾暣閾捐矾鍒嗘瀽

> 鍩轰簬褰撳墠宸ヤ綔鏍?HEAD锛歚9a7f87d refactor(ops): clean workflow naming and docs`銆傛湰鏂囧彧鎻忚堪褰撳墠婧愮爜宸茶惤鍦拌涓猴紝涓嶅杩板巻鍙茶璁¤崏妗堛€?>
> 鐩爣锛氭妸 `ai_ops_stream` / `/ops` 瑙﹀彂鍚庣殑瀹屾暣鏁呴殰澶勭疆 workflow銆佸叧閿姸鎬併€佷富瑕佷唬鐮佽惤鐐瑰拰娴嬭瘯璇佹嵁鏀惧湪涓€浠藉彲杩借釜鏂囨。閲屻€?
## 1. 鎬荤粨缁撹

- 褰撳墠杩愮淮杞ㄥ凡缁忔槸鏄惧紡 Plan -> Execute -> Replan 鏋舵瀯锛屾牳蹇冨惊鐜负锛歚incident_analysis -> diagnosis_gate -> plan -> plan_gate -> plan_approval -> execute_plan -> verify_plan -> replan_decider`锛屽惊鐜粨鏉熷悗杩涘叆 `final_report`銆?- `ExecutionPlan` 鐨?canonical 鏉ユ簮鏄?Graph State 涓殑 `PlanState`锛屼笉鏄?execution 宸ュ叿鍐呴儴缂撳瓨锛沞xecution 宸ュ叿缂撳瓨鍙綔涓轰粠 Graph State 娲剧敓鐨勮繍琛屾湡鐘舵€併€?- `execution_agent` 宸茬槮韬负鎵ц浠ｇ悊锛屽彧鏆撮湶 `execute_step`銆乣validate_result`銆乣rollback`锛屼笉鍐嶆毚闇?`normalize_plan`銆乣generate_plan`銆乣validate_plan`銆?- 瀹℃壒鍒嗕袱灞傦細璁″垝绾?`plan_approval` 缁戝畾鏁翠唤 plan snapshot锛涘懡浠ょ骇楂橀闄╁姩浣滀粛鐢?`execute_step` 瑙﹀彂 interrupt/resume銆?- 閲嶈鍒掔粺涓€鐢?`replan_decider` 鍐欏叆 `ReplanState`锛涢渶瑕侀噸瑙勫垝鏃舵爣鍑嗗寲涓?`refresh_observation`锛屼笅涓€杞粠閲嶆柊瑙傛祴/璇婃柇寮€濮嬨€?- final report 浼氳惤鐩樺埌 `logs/ops_reports`锛屽苟鍦ㄦ弧瓒宠川閲忛棬妲涙椂褰掓。鍒?ops case 鐭ヨ瘑搴擄紝metadata 鍖呭惈 plan/replan/verification 瀛楁銆?
## 2. 澶栭儴鍏ュ彛涓庤繍琛屾椂澶栧３

### 2.1 杩涚▼涓庡簲鐢ㄨ閰?
- `main.go` 鏄敮涓€杩涚▼鍏ュ彛锛屽垵濮嬪寲 Redis/MySQL/ES/妯″瀷鐩稿叧閰嶇疆鍚庤皟鐢?`bootstrap.NewApplication`锛汬TTP 鏈嶅姟缁戝畾 `/api/v1` 骞舵妸 `DialogueAgent`銆乣OpsAgent`銆乣KnowledgeAgent` 娉ㄥ叆 `chat.NewV1WithHooks`銆傝瘉鎹細`main.go:95-118`銆乣main.go:128-140`銆?- `bootstrap.NewApplication` 鍦ㄥ惎鍔ㄦ椂鍒涘缓 `ops.NewIncidentWorkflowAgent`锛屽苟鎶婅繑鍥炵殑 `OpsAgent` 鏀捐繘 `Application`銆傝瘉鎹細`internal/bootstrap/app.go:201-212`銆乣internal/bootstrap/app.go:235-244`銆?- Controller 鍒濆鍖栨椂涓?ops agent 鍒涘缓 `adk.Runner`锛屽惎鐢?streaming锛屽苟鎺ュ叆 Redis checkpoint store锛汻edis 涓嶅彲鐢ㄦ椂 fallback 鍒板唴瀛?checkpoint銆傝瘉鎹細`internal/controller/chat/chat_v1.go:86-133`銆?
### 2.2 HTTP / SSE 鍏ュ彛

- API 瀹氫箟浜嗕袱涓繍缁村叆鍙ｏ細`POST /api/v1/ai_ops_stream` 鍜?`POST /api/v1/ai_ops_resume_stream`銆傝瘉鎹細`api/chat/v1/chat.go:62-78`銆?- `AIOpsStream` 浣跨敤鍥哄畾 `opsDiagnosticPrompt`锛岀敓鎴?`checkpointID := generateCheckpointID("aiops")`锛岃Е鍙?turn hooks锛岀劧鍚庤皟鐢?`opsStreamRunner.Run(...)`銆傝瘉鎹細`internal/controller/chat/chat_v1.go:496-502`銆?- `AIOpsStream` 鐨?SSE 浜嬩欢鍖呮嫭 tool step銆乧ontent銆乮nterrupt銆乨one锛涢亣鍒?interrupt 鏃舵妸 `checkpoint_id` 鍜?interrupt payload 鍐欑粰鍓嶇銆傝瘉鎹細`internal/controller/chat/chat_v1.go:519-552`銆?- `AIOpsResumeStream` 鐢?`checkpoint_id + interrupt_ids + approved/resolved/comment/selection_value` 璋冪敤 `resumeAgent(...)`锛岀劧鍚庣户缁互鍚屼竴 SSE 浜嬩欢妯″瀷杈撳嚭銆傝瘉鎹細`internal/controller/chat/chat_v1.go:555-623`銆?- `/ops` slash 鍛戒护涔熻蛋鍚屼竴涓?ops runner锛屽彧鏄?prompt 鏉ヨ嚜 slash registry 鐨勭粨鏋溿€傝瘉鎹細`internal/controller/chat/chat_v1.go:668-670`銆乣internal/controller/chat/chat_v1.go:730-785`銆?
## 3. 褰撳墠 Workflow 鎷撴墤

婧愮爜閲岀殑鏋勯€犱綅缃槸 `internal/workflow/ops/incident_workflow.go`銆?
```text
incident_workflow_agent
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

鍏抽敭璇佹嵁锛?
- `NewIncidentWorkflowAgent` 鍒涘缓 incident銆乸lan銆乪xecution銆乨iagnosis_gate銆乸lan_gate銆乸lan_approval銆乿erify_plan銆乺eplan_decider銆乫inal_report 绛夋垚鍛樸€傝瘉鎹細`internal/workflow/ops/incident_workflow.go:59-118`銆?- workflow build 鍚庣粦瀹?`incidentHistoryRewriter`锛屽苟瑕佹眰鏈€缁堢粨鏋滀粛鏄?`adk.ResumableAgent`銆傝瘉鎹細`internal/workflow/ops/incident_workflow.go:123-140`銆?- `newIncidentWorkflowTeam` 娉ㄥ唽 9 涓垚鍛橈紝骞舵妸鍓?8 涓斁鍏?`incident_response_loop`锛屾渶鍚庡崟鐙鍔?`incident_final_report_stage`銆傝瘉鎹細`internal/workflow/ops/incident_workflow.go:155-205`銆?- 榛樿 loop 娆℃暟鏉ヨ嚜 `agentteams.DefaultLoopMaxIterations`锛屽綋鍓?workflow config 鏈樉寮忚缃椂璧伴粯璁ゅ€笺€傝瘉鎹細`internal/workflow/ops/incident_workflow.go:15`銆乣internal/workflow/ops/incident_workflow.go:155-158`銆?
## 4. 闃舵绾т唬鐮侀摼璺?
| 闃舵 | 涓昏浠ｇ爜 | 杈撳叆/渚濊禆 | 鐘舵€佸啓鍏ユ垨杈撳嚭 | 閫€鍑?涓嬩竴姝?|
|---|---|---|---|---|
| `incident_analysis` | `NewOpsAgent` | 鐢ㄦ埛闂銆丟raph State銆佽瘖鏂?deferred tools | RCA銆乪vidence銆乺emediation intent/proposal 鍐欏叆 `IncidentState` | 杩涘叆 `diagnosis_gate` |
| `diagnosis_gate` | `newDiagnosisGateAgent` | `IncidentState` 涓殑 RCA/evidence/proposal | `IncidentContractValid`銆乣IncidentContractIssues`锛涘け璐ュ啓 `refresh_observation` | 閫氳繃鍒欒繘鍏?`plan`锛屽け璐ヨ繘鍏ヤ笅涓€杞噸鏂拌瘖鏂?|
| `plan` | `NewPlanAgent` | diagnosis/remediation intent 鍜?Graph State | 鐢熸垚 canonical `ExecutionPlan`锛岀敱 state bridge 鍐欏叆 `PlanState` | 杩涘叆 `plan_gate` |
| `plan_gate` | `newPlanGateAgent` | `PlanState` | `PlanGateState`銆侀闄?闃绘柇鍘熷洜锛涘け璐ュ啓 `refresh_observation` | 閫氳繃鍒欒繘鍏?`plan_approval` |
| `plan_approval` | `newPlanApprovalAgent` | 褰撳墠 `PlanState` + `PlanGateState` | `PlanApprovalState`锛岀粦瀹?snapshot hash | 浣庨闄╄嚜鍔ㄦ壒鍑嗭紱楂橀闄?interrupt锛涙嫆缁濆垯 manual_required |
| `execute_plan` | `NewExecutionAgent` + contract guard | 宸查€氳繃 gate 鍜?approval 鐨?`PlanState` | execution trace銆乻tep validation銆佹墽琛岀姸鎬?| 杩涘叆 `verify_plan` |
| `verify_plan` | `newVerifyPlanAgent` | `PlanState` + execution trace/status | `PlanVerificationState` | 杩涘叆 `replan_decider` |
| `replan_decider` | `newReplanDeciderAgent` | verification/execution/proposal/diagnostic facts | `ReplanState` | `complete` break loop锛沗refresh_observation` 涓嬩竴杞噸鏂拌瘖鏂紱`manual_required` interrupt/break |
| `final_report` | `newFinalReportAgent` | 瀹屾暣 `IncidentState` | Markdown 鎶ュ憡銆乷ps report 鏂囦欢銆佸彲閫夌煡璇嗗簱褰掓。 | workflow 缁撴潫 |

### 4.1 incident_analysis锛氬彧璇昏瘖鏂笌淇鎰忓浘

- `NewOpsAgent` 鐨勬敞閲婃槑纭叾鎷ユ湁 diagnosis銆乪vidence collection銆乺oot-cause reasoning 鍜?remediation proposal generation锛涘畠閫氳繃 deferred diagnostic tools 鎸夐渶鍙栬瘉銆傝瘉鎹細`internal/workflow/ops/agent.go:30-47`銆?- `ops_incident_agent` 浣跨敤 `RoleOps` prompt 鍜?compact middleware锛屽伐鍏风綉鍏崇敱 `toolkit.BuildDeferredGatewayEinoTools` + permissions checker 缁熶竴鍖呰銆傝瘉鎹細`internal/workflow/ops/agent.go:49-64`銆?- `RoleOps` prompt 鏄庣‘锛氬畠鏄?`plan_agent` 涓婃父锛屽彧杈撳嚭 DiagnosisState/璇婃柇鎽樿/remediation_intent锛屼笉鐢熸垚鏈€缁堝懡浠わ紝涓嶈皟鐢ㄥ啓鎿嶄綔銆傝瘉鎹細`internal/prompt/role_prompts.go:120-155`銆?
### 4.2 diagnosis_gate锛氳瘖鏂绾﹂棬

- `newDiagnosisGateAgent` 鐨?agent name 鏄?`diagnosis_gate`锛岃亴璐ｆ槸鏍￠獙杩涘叆 planning 鍓嶇殑 incident diagnosis evidence銆傝瘉鎹細`internal/workflow/ops/diagnosis_gate.go:22-38`銆?- gate 浼氳皟鐢?`validateIncidentDiagnosis(...)`锛岀劧鍚庨€氳繃 `applyIncidentContractValidationForGate(...)` 鍐欏叆鐘舵€侊紱澶辫触鏃惰緭鍑?blocked 淇℃伅銆傝瘉鎹細`internal/workflow/ops/diagnosis_gate.go:43-70`銆?- 璇婃柇澶辫触浼氳缃?`ValidationBlocked=true`銆乣ValidationRisk=contract_invalid`锛屽苟鍐欏叆 `ReplanState(decision=refresh_observation)`銆傝瘉鎹細`internal/workflow/ops/diagnosis_gate.go:337-358`銆?
### 4.3 plan锛氱敓鎴?canonical ExecutionPlan

- `NewPlanAgent` 鍙毚闇?planner 宸ュ叿锛歚normalize_plan`銆乣generate_plan`銆乣validate_plan`锛涗笉鏆撮湶鎵ц/鍥炴粴宸ュ叿銆傝瘉鎹細`internal/workflow/ops/plan_agent.go:28-43`銆?- `plan_agent` 浣跨敤 `RolePlan` prompt锛屽苟閫氳繃 compact middleware 杩愯銆傝瘉鎹細`internal/workflow/ops/plan_agent.go:47-63`銆?- state bridge 鍦?`stage == "plan"` 鏃惰В鏋?generated execution plan 骞惰皟鐢?`applyExecutionPlanState`銆傝瘉鎹細`internal/workflow/ops/state_bridge.go:334-338`銆?- `applyExecutionPlanState` 璁＄畻 snapshot hash銆佺淮鎶?revision锛屽苟鎶婅鍒掗暅鍍忓埌鏃у吋瀹瑰瓧娈?`ExecutionPlanID/Desc/Risk/Steps`锛涜鍒掑彉鏇存椂浼氫娇鏃?approval 鍙樺洖 pending銆傝瘉鎹細`internal/workflow/ops/state_bridge.go:494-547`銆?
### 4.4 plan_gate锛氳鍒掔骇瀹夊叏闂?
- `newPlanGateAgent` 浠?Graph State 鍙?canonical plan锛屼笉鎺ユ敹涓存椂 remediation proposal銆傝瘉鎹細`internal/workflow/ops/plan_gate.go:22-40`銆?- `validateCanonicalPlan` 鎶?`PlanState` 杞垚 execution tool 鐨?`ExecutionPlan`锛屽啀璋冪敤 `executiontools.ValidateExecutionPlan`銆傝瘉鎹細`internal/workflow/ops/plan_gate.go:62-73`銆?- plan gate 缁撴灉鍐欏叆 `PlanGateState`锛涘綋 plan invalid/blocked 鏃讹紝鍐欏叆 `ReplanState(decision=refresh_observation, source=plan_gate)`銆傝瘉鎹細`internal/workflow/ops/state_bridge.go:549-582`銆?
### 4.5 plan_approval锛氭暣浠借鍒掑揩鐓у鎵?
- `newPlanApprovalAgent` 鏄?`adk.ResumableAgent`锛岀敤浜庡湪鎵ц鍓嶇粦瀹氭暣浠?canonical plan snapshot銆傝瘉鎹細`internal/workflow/ops/plan_gate.go:120-138`銆?- 濡傛灉 plan 鏈?ready锛宎pproval stage 浼氳烦杩囷紱濡傛灉褰撳墠 plan 宸叉壒鍑嗭紝鐩存帴缁х画锛涘鏋滈渶瑕佸鎵癸紝鍒欏啓 pending 骞跺彂鍑?`plan_approval_required` interrupt銆傝瘉鎹細`internal/workflow/ops/plan_gate.go:143-172`銆?- Resume 鏃堕€氳繃 `parsePlanApprovalDecision` 瑙ｆ瀽鐢ㄦ埛鎭㈠鏁版嵁锛涜嫢 pending snapshot 涓庡綋鍓?plan 涓嶅尮閰嶏紝浼氳浆 `manual_required` 骞?break loop锛岄伩鍏嶅鐢ㄦ棫瀹℃壒銆傝瘉鎹細`internal/workflow/ops/plan_gate.go:177-219`銆?- `planReadyForApproval` 寮哄埗瑕佹眰瀛樺湪 current plan銆乸lan gate 宸查獙璇併€乬ate snapshot 鏈繃鏈熴€佷笖 gate 鏈?blocked銆傝瘉鎹細`internal/workflow/ops/plan_gate.go:221-235`銆?
### 4.6 execute_plan锛氬彧鎵ц宸叉壒鍑嗚鍒?
- `NewExecutionAgent` 鍙垱寤?`execute_step`銆乣validate_result`銆乣rollback` 涓変釜 deferred tools銆傝瘉鎹細`internal/execution/agent.go:34-71`銆?- prompt 鏄庣‘ execution agent 浠?Graph State 娑堣垂宸查€氳繃 `plan_gate` 鍜?`plan_approval` 鐨?canonical plan锛屽苟涓嶅緱鐢熸垚/淇敼 `ExecutionPlan`銆傝瘉鎹細`internal/execution/agent.go:52-57`銆乣internal/prompt/role_prompts.go:213-224`銆乣internal/prompt/sections.go:154-159`銆?- `newContractGuardedExecutionAgent` 鍦?execution 鍓嶅啀娆℃鏌?diagnosis contract銆丳lanState銆丳lanGateState銆乤pproval 鏄惁婊¤冻锛涢€氳繃鍚庣敤 `PrepareApprovedExecutionPlanFromGraphState` seed 鎵ц宸ュ叿鐘舵€併€傝瘉鎹細`internal/workflow/ops/diagnosis_gate.go:74-110`銆乣internal/workflow/ops/diagnosis_gate.go:180-210`銆?- `execute_step` 鑷韩涔熸鏌?approved plan 鏄惁 prepared銆乿alidated锛涙湭鍑嗗鎴栨湭楠岃瘉浼氱洿鎺ュけ璐ャ€傝瘉鎹細`internal/execution/tools/execute_step.go:216-221`銆?- state bridge 闃插尽 execute stage 瓒婃潈锛氬鏋?`execute_plan` 杈撳嚭浜嗘柊鐨?execution plan锛屼細鏍囪 `manual_required`锛屽苟璁板綍 boundary violation銆傝瘉鎹細`internal/workflow/ops/state_bridge.go:344-363`銆?
### 4.7 verify_plan锛氬叏璁″垝楠岃瘉

- `newVerifyPlanAgent` 璇诲彇褰撳墠 state锛屾瀯閫?verification payload锛屽啓鍏?`PlanVerificationState`锛屽苟杈撳嚭 JSON銆傝瘉鎹細`internal/workflow/ops/incident_nodes.go:61-99`銆?- `buildPlanVerificationPayload` 鍦?success 鏃惰姹?execution trace 瑕嗙洊 canonical plan锛涘鏋滄病鏈?plan銆乵anual_required銆乺eplan_required銆乫ailed 鎴栨病鏈?terminal status锛岄兘浼氳浆鎴愬け璐ユ垨 manual_required銆傝瘉鎹細`internal/workflow/ops/incident_nodes.go:104-168`銆?- `buildReadablePlanVerificationLine` 浼氭妸 verify result 鍐欐垚 final report 鍙鎽樿銆傝瘉鎹細`internal/workflow/ops/incident_nodes.go:1248-1263`銆?
### 4.8 replan_decider锛氬敮涓€閲嶈鍒掑嚭鍙?
- `replan_decider` 鐨?`Run` 鑱氬悎 proposal銆乪xecution plan銆乿alidation銆乻tep validation銆乪xecution result銆乨iagnostic insight 绛変簨瀹炪€傝瘉鎹細`internal/workflow/ops/incident_nodes.go:429-450`銆?- 瀵?execution boundary violation锛屼細鐩存帴鍐?`manual_required` 骞?break loop銆傝瘉鎹細`internal/workflow/ops/incident_nodes.go:451-458`銆?- plan validation blocked 鎴?requires confirmation 浼氬啓 `manual_required` 骞跺彂 interrupt銆傝瘉鎹細`internal/workflow/ops/incident_nodes.go:460-510`銆?- `validate_result` 瑕佹眰 replan 鏃讹紝浼氳褰曢噸澶嶉棶棰橈紱鏈揪鍒伴槇鍊煎垯鍐?`refresh_observation`锛岃揪鍒伴槇鍊煎垯杞汉宸ャ€傝瘉鎹細`internal/workflow/ops/incident_nodes.go:512-528`銆乣internal/workflow/ops/incident_nodes.go:386-417`銆?- execution success 浣嗕粛鏈?actionable issues 鎴栨病鏈?executed_steps锛屼細鍥炲埌 `refresh_observation`锛涚湡姝?success 鎵嶅啓 `complete` 骞?break loop銆傝瘉鎹細`internal/workflow/ops/incident_nodes.go:592-632`銆?- 榛樿鍏滃簳澶辫触浼氬啓 `refresh_observation` 骞舵彁绀哄洖鍒?ops incident agent銆傝瘉鎹細`internal/workflow/ops/incident_nodes.go:658-667`銆?- Resume 涓敤鎴风‘璁ゅ凡瑙ｅ喅鏃讹紝鍐?`complete` 骞?break loop 杩涘叆 final report銆傝瘉鎹細`internal/workflow/ops/incident_nodes.go:672-700`銆?
### 4.9 final_report锛氭姤鍛婅惤鐩樹笌鐭ヨ瘑褰掓。

- `finalReportAgent.Run` 璇诲彇 Graph State锛屾帹鏂?final status锛岀敓鎴?summary锛屽啓鍥?state锛屽苟璋冪敤 `persistFinalOpsReport`锛涜川閲忔弧瓒虫椂璋冪敤 `archiveFinalOpsReport`銆傝瘉鎹細`internal/workflow/ops/incident_nodes.go:761-795`銆?- 鎶ュ憡姝ｆ枃鐢?`buildFinalOpsSummary` 鐢熸垚锛屽寘鍚彂鐜扮殑闂銆佸師鍥犮€佸缃繃绋嬬瓑鍙娈佃惤銆傝瘉鎹細`internal/workflow/ops/incident_nodes.go:800-850`銆?- `persistFinalOpsReport` 鍐欏叆 `logs/ops_reports`锛宖ront matter 鍖呭惈 `plan_id`銆乣plan_revision`銆乣verification_status`銆乣replan_count`銆乣terminal_decision` 绛夊瓧娈点€傝瘉鎹細`internal/workflow/ops/incident_nodes.go:1662-1693`銆?- `archiveFinalOpsReport` 灏嗘姤鍛婁綔涓?`ops_final_report` document 鍐欏叆 Milvus ops collection锛宮etadata 鍚屾牱鍖呭惈 plan/replan/verification 瀛楁銆傝瘉鎹細`internal/workflow/ops/incident_nodes.go:1696-1745`銆?
## 5. Graph State 涓庡巻鍙插帇缂?
- Graph State 鐨?session key 鏄?`incident_graph_state`锛沗PlanState`銆乣PlanGateState`銆乣PlanVerificationState`銆乣ReplanState`銆乣PlanApprovalState` 閮戒細 gob register锛屾敮鎸?checkpoint/resume銆傝瘉鎹細`internal/workflow/ops/state_bridge.go:17-33`銆?- `IncidentState` 鍚屾椂淇濆瓨 observation/RCA/remediation銆乧anonical plan銆乬ate銆乤pproval銆乪xecution trace銆乿erification銆乺eplan銆乫inal report 绛夊瓧娈点€傝瘉鎹細`internal/workflow/ops/state_bridge.go:35-108`銆?- `stateBridgeAgent.updateByStage` 鎸?stage 瑙ｆ瀽杈撳嚭骞跺啓鍏ョ姸鎬侊細incident 鍐?RCA/proposal锛宲lan 鍐?PlanState锛宲lan_gate 鍐?PlanGateState锛寁erify_plan 鍐?PlanVerification锛宔xecute_plan 绂佹鏀瑰啓 plan銆傝瘉鎹細`internal/workflow/ops/state_bridge.go:310-363`銆?- `invalidateCurrentPlanForObservationRefresh` 浼氭竻绌哄綋鍓?plan/gate/verification/approval/execution trace锛屼繚璇侀噸瑙勫垝蹇呴』閲嶆柊瑙傛祴/璇婃柇鍚庡啀鐢熸垚鏂拌鍒掋€傝瘉鎹細`internal/workflow/ops/state_bridge.go:750-770`銆?- `normalizeReplanDecision` 灏?success/done 褰掍竴涓?`complete`锛屽皢 replan/retry/revise_plan 褰掍竴涓?`refresh_observation`锛屽皢 blocked/approval_required 褰掍竴涓?`manual_required`銆傝瘉鎹細`internal/workflow/ops/state_bridge.go:772-785`銆?- `incidentHistoryRewriter` 鍙繚鐣欐渶鏂扮敤鎴疯緭鍏?+ 绮剧畝 Graph State锛屼笉鎶婂畬鏁?transcript 鍜屽ぇ鏃ュ織鍙嶅鍥炵亴妯″瀷銆傝瘉鎹細`internal/workflow/ops/state_bridge.go:873-891`銆?- `renderIncidentState` 杈撳嚭 compact state锛屽寘鎷?plan/gate/verification/replan/approval/execution/final 绛夊叧閿瓧娈点€傝瘉鎹細`internal/workflow/ops/state_bridge.go:908-970`銆?
## 6. 瀹夊叏杈圭晫

### 6.1 璁″垝绾ц竟鐣?
- 娌℃湁 canonical plan銆佹病鏈?plan gate銆乬ate 杩囨湡/blocked銆佹垨褰撳墠 plan 鏈鎵规椂锛宍executionGuardAllowsExecution` 閮戒細鎷掔粷鎵ц銆傝瘉鎹細`internal/workflow/ops/diagnosis_gate.go:180-199`銆?- plan snapshot 鍙樺寲浼氫娇 approval 鐘舵€佸洖鍒?pending锛岄伩鍏嶆棫瀹℃壒澶嶇敤鍒版柊璁″垝銆傝瘉鎹細`internal/workflow/ops/state_bridge.go:494-547`銆?
### 6.2 鍛戒护绾ц竟鐣?
- `execute_step` 鍙湁鍦?approved plan 宸?prepared 涓?validated 鍚庢墠鍏佽鎵ц銆傝瘉鎹細`internal/execution/tools/execute_step.go:216-221`銆?- `execute_step` 浣跨敤 command whitelist 鍜?permissions checker锛涘彉鏇寸被鍛戒护閫氳繃 `ExecutionApprovalInterruptInfo` 瑙﹀彂鐢ㄦ埛纭銆傝瘉鎹細`internal/execution/tools/execute_step.go:24-44`銆?
### 6.3 寰幆涓庝汉宸ユ帴绠¤竟鐣?
- 閲嶅闂闃堝€间负 `maxRepeatedIssueRetries = 3`锛涜揪鍒伴槇鍊间細鍐?`manual_required` 骞?break loop銆傝瘉鎹細`internal/workflow/ops/incident_nodes.go:21`銆乣internal/workflow/ops/incident_nodes.go:386-417`銆?- `replan_decider` 鏄?replan/manual/complete 鐨勭粺涓€鍑哄彛锛岄伩鍏嶅涓?stage 鍚勮嚜鍐冲畾鏈€缁堟帶鍒舵祦銆傝瘉鎹細`internal/workflow/ops/incident_nodes.go:419-667`銆?
## 7. Prompt 涓庡伐鍏锋毚闇查潰

- `RoleOps` prompt 鏄庣‘ incident agent 鍙礋璐ｈ瘖鏂笌淇鎰忓浘锛屼笉鐢熸垚鏈€缁堝懡浠ゃ€傝瘉鎹細`internal/prompt/role_prompts.go:120-155`銆?- `RoleExecution` prompt 鏄庣‘ execution agent 涓嶅緱鐢熸垚鏂扮殑 ExecutionPlan锛屼笉寰椾慨鏀?plan_id/姝ラ/椋庨櫓/鍥炴粴绛栫暐銆傝瘉鎹細`internal/prompt/role_prompts.go:213-224`銆?- prompt section 涓?execution deferred tools 鍙垪鍑?`execute_step`銆乣validate_result`銆乣rollback`锛屽苟鏄庤璁″垝鑱岃矗灞炰簬 `plan_agent/plan_gate`銆傝瘉鎹細`internal/prompt/sections.go:154-159`銆?
## 8. 娴嬭瘯瑕嗙洊璇佹嵁

- workflow stage 缁撴瀯娴嬭瘯鐩存帴鏂█ `incident_response_loop` 鎴愬憳椤哄簭涓?`incident_analysis, diagnosis_gate, plan, plan_gate, plan_approval, execute_plan, verify_plan, replan_decider`銆傝瘉鎹細`internal/workflow/ops/incident_workflow_test.go:54`銆?- execute stage 涓嶅厑璁歌鐩?canonical plan锛氭祴璇曟瀯閫?execute_plan 杈撳嚭鏂?plan锛屽苟鏂█鍘?`PlanState` 涓嶈瑕嗙洊涓?execution status 鍙樹负 `manual_required`銆傝瘉鎹細`internal/workflow/ops/incident_workflow_test.go:405-416`銆?- plan gate / execution guard 娴嬭瘯瑕嗙洊缂?gate 鎷掔粷銆乤pproved canonical plan 鏀捐銆傝瘉鎹細`internal/workflow/ops/plan_gate_test.go:16-70`銆?- plan approval parser 娴嬭瘯瑕嗙洊鍚﹀畾/闈炲鎵硅緭鍏ヤ笉浼氳璇垽涓?approved锛屾樉寮?approve 鎵嶉€氳繃銆傝瘉鎹細`internal/workflow/ops/plan_gate_test.go:104-124`銆?- final report 娴嬭瘯瑕嗙洊 verify_plan failure line銆乫ront matter 涓殑 `plan_id`銆乣replan_count`銆乣terminal_decision`銆傝瘉鎹細`internal/workflow/ops/diagnosis_gate_test.go:129-173`銆乣internal/workflow/ops/diagnosis_gate_test.go:234-240`銆?- prompt 娴嬭瘯瑕嗙洊 execution prompt 蹇呴』鍖呭惈 execute_plan stage/execute_step/validate_result/rollback锛屽苟涓嶅緱鏆撮湶 planning tools銆傝瘉鎹細`internal/prompt/builder_test.go:89-100`銆?
## 9. 褰撳墠宸茬煡杈圭晫涓庨渶瑕佹敞鎰忕殑鐐?
- 杩欐槸婧愮爜涓庡崟鍏冩祴璇曞眰闈㈢殑閾捐矾鍒嗘瀽锛涚湡瀹?Kubernetes/Prometheus/ES 鐜涓嬬殑绔埌绔?smoke 浠嶉渶鍗曠嫭杩愯 `ai_ops_stream -> interrupt/resume -> final_report` 鏉ラ獙璇佸閮ㄧ郴缁熷彲鐢ㄦ€с€?- ADK team 褰撳墠鐢?`LoopStage` 鎵胯浇瀹屾暣 Plan/Execute/Replan 閾撅紝涓嶆槸鍥炬暟鎹簱寮忔潯浠惰竟锛沗replan_decider` 閫氳繃 assistant/break/interrupt event 褰卞搷 loop 鏄惁缁х画鎴栫粓姝€?- `rca_agent`銆乣strategy_agent`銆乣observation_collector` 鐩稿叧浠ｇ爜浠嶅彲鑳戒綔涓轰繚鐣?宸ュ叿鍖栧疄鐜板瓨鍦紝浣嗗綋鍓嶄富 workflow 鐨勮繍琛岄摼璺互涓婅堪 `incident_workflow.go` 涓哄噯銆?- `docs/agent-宸ヤ綔娴佸眰鏍稿鎶ュ憡.md` 宸叉爣璁颁负鍘嗗彶鎶ュ憡锛岄噷闈㈢殑鏃?stage 鍚嶇敤浜庤拷婧紝涓嶄唬琛ㄥ綋鍓嶉摼璺€?
## 10. 涓€鍙ヨ瘽鍙ｅ緞

褰撳墠 OnCall 杩愮淮 workflow 鏄竴涓彲鎭㈠銆佸彲瀹℃壒銆佸彲瀹¤鐨勬晠闅滃缃祦姘寸嚎锛氬厛鐢?`incident_analysis` 鏈€灏忓寲鍙栬瘉骞跺舰鎴愯瘖鏂紝鍐嶇敱 `plan` 鐢熸垚 canonical ExecutionPlan锛岀粡 `plan_gate` 涓?`plan_approval` 缁戝畾璁″垝瀹夊叏杈圭晫锛宍execute_plan` 鍙秷璐瑰凡鎵瑰噯璁″垝锛宍verify_plan` 鍋氬叏璁″垝楠屾敹锛宍replan_decider` 缁熶竴鍐冲畾瀹屾垚銆侀噸鏂拌娴嬫垨浜哄伐鎺ョ锛屾渶鍚?`final_report` 浜у嚭鍙惤鐩樺拰鍙绱㈢殑鎶€鏈姤鍛娿€?


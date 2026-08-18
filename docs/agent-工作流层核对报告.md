# Agent 涓庡伐浣滄祦灞傛牳瀵规姤鍛婏紙浠诲姟 ta575a1f9锛?
> 鍘嗗彶鎶ュ憡锛氭湰鏂囪褰曠殑鏄?Plan/Execute/Replan 閲嶆瀯鍓嶇殑闃舵鎬у璁″揩鐓э紝淇濈暀鏃?stage 鍚嶇О鐢ㄤ簬杩芥函銆傚綋鍓嶆簮鐮佺湡瀹為摼璺互 `docs/PROJECT_STRUCTURE.md` 鍜?`internal/workflow/ops/incident_workflow.go` 涓哄噯銆?
> 鍩轰簬 D:/Code/project/oncall 褰撳墠浠ｇ爜锛坢odule `go_agent`锛孒EAD=8e7e922锛夛紝浠ｇ爜瀹炴祴锛屼笌鍘熸枃妗?docs/椤圭洰浠嬬粛.md 閫愭潯瀵圭収銆傜敓鎴愭椂闂达細瑙?git log HEAD銆?
## 1. Agent 鐩綍缁撴瀯锛坕nternal/agent/锛? 涓瓙鐩綍锛?
| 鐩綍          | 鑱岃矗锛堝疄娴嬶級                                                                                                            |
| ------------- | ----------------------------------------------------------------------------------------------------------------------- |
| `dialogue/`   | 瀵硅瘽 Agent锛堟剰鍥惧垎鏋?+ 宸ュ叿缂栨帓锛?                                                                                      |
| `ops/`        | 鏁呴殰澶勭疆鏍稿績锛歸orkflow 缂栨帓銆乪xecution_gate銆乮ncident_contract_gate銆乫inal_reporter銆乷bservation_collector銆丳od 鏃ュ織鈫扙S |
| `rca/`        | 鏍瑰洜鍒嗘瀽锛?*宸茶劚绂诲伐浣滄祦**锛屾棤璋冪敤鏂癸級                                                                                  |
| `execution/`  | 鍛戒护绾ц鍒?鏍￠獙/鎵ц/鍥炴粴锛堝伐浣滄祦瀹為檯浣跨敤锛?                                                                            |
| `strategy/`   | 绛栫暐璇勪及/浼樺寲/鐭ヨ瘑鏇存柊锛?*宸茶劚绂诲伐浣滄祦**锛屾棤璋冪敤鏂癸級                                                                    |
| `knowledge/`  | 鐭ヨ瘑涓婁紶涓撶敤 Chain                                                                                                      |
| `agentteams/` | **鏂板**锛堟彁浜?0a72b06锛夛細鍥㈤槦/澶氶樁娈靛伐浣滄祦澹版槑寮忔瀯寤?                                                                  |
| `slash/`      | **鏂板**锛堟彁浜?a49ead7锛夛細slash 鍛戒护璺敱                                                                                |
| `toolkit/`    | **鏂板**锛氱粺涓€宸ュ叿娉ㄥ唽/缃戝叧/鏉冮檺/hook                                                                                   |

鍙︽敞鎰忥細`ops/` 涓嬭繕鏈?`incident_observer.go`锛坥bservation_collector锛夈€乣integration.go`锛圛ntegratedOpsExecutor锛夈€乣pod_log_shipper.go`锛圥od 鏃ュ織鈫扙S 閲囬泦锛夈€乣state_bridge.go`锛圙raph State锛夈€乣incident_contract*.go`锛堝绾︽牎楠岋級銆?
## 2. 宸ヤ綔娴佺紪鎺掞紙鏈€澶у彉鍖栫偣锛屼笌鏂囨。鐭涚浘锛?
鏂囦欢锛歚internal/workflow/ops/incident_workflow.go`锛屽叧閿嚱鏁?`NewIncidentWorkflowAgent`锛堣 56锛夈€?
褰撳墠褰㈡€侊細

```
Sequential(
  Loop(incident, incident_contract_gate, execution, gate),  // 鏈€澶?MaxExecutionLoops 娆?  final_report
)
```

- 鐢?`newIncidentWorkflowTeam`锛堣 132锛夌敤 agentteams 鏋勫缓锛歚AddLoopStage("incident_response_loop", ..., maxLoops, "incident","incident_contract_gate","execution","gate")` + `AddSequentialStage("incident_final_report_stage", ..., "final_report")`銆?- 5 涓垚鍛橈細incident(ops_incident_agent)銆乮ncident_contract_gate銆乪xecution銆乬ate銆乫inal_report銆?
### 涓庡師鏂囨。鐨勭煕鐩剧偣

1. 鏂囨。 4.3 鑺?`Sequential(observation_collector, rca_agent, Loop(ops_agent, execution_agent, execution_gate), strategy_agent, final_reporter)` **宸蹭笉鎴愮珛**銆?   - `newObservationCollectorAgent`锛坕ncident_observer.go锛夈€乣NewRCAAgent`锛坮ca/agent.go锛夈€乣NewStrategyAgent`锛坰trategy/agent.go锛夊湪宸ヤ綔娴佷腑**鍧囨棤寮曠敤**锛沚ootstrap/app.go 鍙垵濮嬪寲 dialogue 涓?knowledge agent銆?   - 瑙傛祴/RCA 鑱岃矗骞跺叆 ops_incident_agent锛堟彁绀鸿瘝 RoleOps锛岃瘖鏂伐鍏烽泦鍐呭祵 rca 宸ュ叿锛夛紱strategy 澶嶇洏涓嶅啀鍑虹幇鍦ㄧ紪鎺掗噷銆?2. **鏂板 incident_contract_gate 鑺傜偣**锛坄internal/workflow/ops/incident_contract_gate.go`锛夛細鎵ц鍓嶆牎楠?RCA 璇佹嵁涓庝慨澶嶆彁妗堝绾︼紙缂哄け鏍瑰洜/璇佹嵁 0/缃俊搴?0.35/楂橀闄╃己 fallback 绛夛級锛屼笉閫氳繃鎵撳洖 ops 閲嶈鍒掞紱execution 澶栧眰鍖?`newContractGuardedExecutionAgent`锛堣 74锛夐槻缁曡繃銆?3. **Loop 榛樿杩唬 3 娆★細鏂囨。浠嶆纭?*銆俙internal/workflow/agentteams/types.go:14` `DefaultLoopMaxIterations = 3`锛岀粡 `incident_workflow.go:15` `incidentDefaultMaxExecutionLoops` 浼犲叆锛沗cfg.MaxExecutionLoops<=0` 鏃跺彇 3銆?4. `incident_nodes.go`锛歟xecution_gate锛坄newExecutionGateAgent`锛岃 33锛夋垚鍔熲啋break loop锛涘け璐モ啋閲嶈鍒掞紱step_validation 瑙﹀彂 replan锛涢噸澶嶉棶棰樿揪闃堝€硷紙`maxRepeatedIssueRetries = 3`锛岃 20锛夆啋杞汉宸ワ紙`buildRepeatedIssueStopEvent`锛岃 150锛夛紱瀹℃壒涓柇璧?`Resume`锛堣 387锛夈€俧inal_reporter锛坄newFinalReportAgent`锛岃 455锛変粠 Graph State 鐢熸垚鎶ュ憡骞跺綊妗ｇ煡璇嗗簱銆?5. `state_bridge.go`锛歚wrapWithIncidentState("incident"/"execution")` 灏嗙粨鏋勫寲缁撴灉鍐欏叆 session values 闃插ぇ鏃ュ織鍥炵亴锛沗IncidentState`锛堣 28锛夌浉姣旀枃妗ｆ柊澧?`IncidentContractValid/Issues`銆乣RepeatedIssue*`銆乣ExecutionPlan*`銆乣ValidationBlocked/Risk` 绛夊瓧娈点€?
## 3. 鍚?Agent 鑱岃矗涓庡伐鍏烽泦

### dialogue agent锛坄internal/workflow/dialogue/agent.go`锛宍NewDialogueAgent`锛?
- 宸ュ叿闆嗭紙`buildDialogueTools`锛岃 122锛? 涓笌鏂囨。涓€鑷淬€佸叏閮ㄤ粛鍦細
  - `intent_analysis`锛圛ntentAnalysisTool.go锛夈€乣request_detail_selection`锛坉etail_selection_tool.go锛夈€乣knowledge_retrieve`锛圞nowledgeRetrieveTool.go锛夈€乣ops_case_retrieve`锛圤psCaseRetrieveTool.go锛夈€乣bash_execute_with_approval`锛圔ashApprovalTool.go锛夈€乣web_search`锛圵ebSearchTool.go锛夈€乣k8s_monitor`銆乣metrics_collector`锛堝悗涓よ€呯敱 `NewDialogueK8sMonitorTool`/`NewDialogueMetricsCollectorTool` 杞彂鍒?ops/tools锛夈€?- 娉ㄦ剰锛歬8s/metrics 鍦?kubeconfig/Prom 涓嶅彲鐢ㄦ椂闄嶇骇 warn 鑰岄潪鎶ラ敊锛沝ialogue 鐢?`toolkit.BuildAlwaysEinoTools`锛堝惈閫氱敤鏂囦欢宸ュ叿锛夈€?
### rca agent锛坄internal/agent/rca/agent.go`锛宍NewRCAAgent`锛?
- 宸ュ叿 7 涓細`k8s_monitor`銆乣metrics_collector`锛堝鐢?ops/tools锛夈€乣time_query`銆乣build_dependency_graph`銆乣correlate_signals`銆乣infer_root_cause`銆乣analyze_impact`銆?- 杈撳嚭濂戠害 `RCAReport`锛坄ops/incident_contract.go:18`锛夛細`root_cause`/`target_node`/`path`/`impact`/`confidence`/`evidence`鈥斺€斾笌鏂囨。 5.4 鑺備竴鑷淬€?- **褰撳墠鏃犺皟鐢ㄦ柟**锛屼粎鐙珛淇濈暀銆?
### ops agent锛坄internal/workflow/ops/agent.go`锛宍NewOpsAgent`锛屽悕 `ops_incident_agent`锛?
- 浜у嚭 `RemediationProposal`锛坄incident_contract.go:39`锛夛細proposal_id/summary/root_cause/target_node/risk_level/actions[]锛坓oal/command_hint/success_criteria/rollback_hint/read_only锛?fallback_plan銆?- 宸ュ叿 8 涓叏 deferred锛坄ops/diagnostic_toolset.go` `BuildDeferredTools`锛夛細k8s_monitor/metrics_collector/es_log_query/time_query/build_dependency_graph/correlate_signals/infer_root_cause/analyze_impact銆?- `defaultOpsIncidentAgentMaxIterations = 48`锛坅gent.go:28锛夈€?
### execution agent锛坄internal/execution/agent.go`锛宍NewExecutionAgent`锛?
- 宸ュ叿閾?6 涓紙鏂囨。 5.6 鑺傛纭級锛歚normalize_plan`鈫抈generate_plan`鈫抈validate_plan`鈫抈execute_step`鈫抈validate_result`鈫抈rollback`锛堝潎 deferred锛夈€?- `defaultExecutionAgentMaxIterations = 96`锛坅gent.go:32锛夈€?- 宸ヤ綔娴佸灞傚寘 `newContractGuardedExecutionAgent`銆?
### strategy agent锛坄internal/agent/strategy/agent.go`锛宍NewStrategyAgent`锛?
- 宸ュ叿 4 涓細`evaluate_strategy`/`optimize_strategy`/`update_knowledge`/`prune_knowledge`銆?- **鏈帴鍏ュ伐浣滄祦**锛堟枃妗?5.8 鑺?鍙備笌澶嶇洏"涓嶆垚绔嬶級銆?
### knowledge agent锛坄internal/knowledge/agent.go`锛宍NewKnowledgeAgent`锛?
- 涓婁紶涓撶敤 Agent锛堥潪鏂囦欢鏈嶅姟锛夛紝`BuildKnowledgeUploadChain`锛坥rchestration.go:59锛夎蛋 file_loader鈫抦arkdown_splitter鈫抦ilvus_indexer锛岃緭鍑哄垎鐗?ID銆傛枃妗?5.2 鑺傚熀鏈纭€?
## 4. 鏂板妯″潡

### agentteams/锛堟彁浜?0a72b06锛?
- `types.go`锛歚Team`/`Member`/`Stage`锛圫tageSequential/StageLoop锛宍DefaultLoopMaxIterations = 3`锛夈€?- `builder.go`锛歚Build` 缂栬瘧涓?Eino ADK `NewSequentialAgent`/`NewLoopAgent`銆?- 浣滅敤锛歩ncident workflow 浠庢墜鍐欑紪鎺掓敼涓?鎴愬憳娉ㄥ唽+闃舵澹版槑"锛宭oop 涓婇檺澶嶇敤甯搁噺淇濊瘉纭畾鎬с€?
### slash/锛堟彁浜?a49ead7锛?
- `parser.go`锛坄Parse` 璇嗗埆 `/cmd args`锛夈€乣registry.go`锛坄Registry` 鍛戒护+鍒悕+鍐茬獊妫€娴嬶級銆乣builtin.go`锛坄CreateDefaultRegistry`锛夈€乣loader.go`锛坒rontmatter 瑙ｆ瀽锛屽姞杞?`.oncall/commands` SourceProject 涓?`.mewcode/commands` SourceMewCompat锛宍$ARGUMENTS` 鏇挎崲锛夈€?- 鍐呯疆 13 涓懡浠わ紙builtin.go:29-47锛夛細
  - 鏈湴/淇℃伅锛歚/help`(h)銆乣/commands`銆乣/status`(s)銆乣/hooks`(hook)銆乣/session`銆乣/memory`
  - prompt 绫伙細`/review`銆乣/diagnose`(diag)銆乣/k8s`(pods)銆乣/metrics`(prom)銆乣/logs`(last-error,errors)銆乣/cases`
  - ops 宸ヤ綔娴佺被锛歚/ops`(incident,aiops)锛圱ypeOpsWorkflow 瑙﹀彂瀹屾暣 AI 杩愮淮澶勭疆宸ヤ綔娴侊級
  - 瀹㈡埛绔姩浣滐細`/clear`
- 娑堣垂鏂癸細`internal/controller/chat/chat_v1.go:635` `handleSlashCommand`銆?
## 5. 宸ュ叿闆?toolkit/

- `types.go`锛歚Tool` 鎺ュ彛锛圢ame/Description/Category/Schema/Execute锛孋ategory 鍒?read/write/command锛? `Registry`锛坉eferred 闆嗗悎 + 鎸?session 鐨勫彂鐜扮姸鎬侊級銆?- `gateway.go`锛歚ToolSearch`锛坰elect: 绮剧‘鎴栧叧閿瘝妫€绱?deferred 宸ュ叿锛変笌 `InvokeDeferredTool`锛堣皟鐢ㄥ凡鍙戠幇 deferred 宸ュ叿锛岄摼璺?pre-hook鈫抪ermission check鈫掑鎵逛腑鏂啋鎵ц鈫抪ost-hook锛夈€?- `adapter.go`锛歚EinoAdapter` 閫傞厤 eino BaseTool锛涗袱涓瀯寤哄叆鍙ｂ€斺€擿BuildAlwaysEinoTools`锛堝惈閫氱敤鏂囦欢宸ュ叿锛宒ialogue/rca/strategy 鐢級涓?`BuildDeferredGatewayEinoTools`锛堝彧鏆撮湶 ToolSearch/InvokeDeferredTool 涓?always 宸ュ叿銆佹棤鏂囦欢缂栬緫鑳藉姏锛宱ps/execution 鐢紝浣滀负鎵ц瀹夊叏杈圭晫锛夈€?- 閫氱敤鏂囦欢宸ュ叿锛圥ascalCase锛夛細`ReadFile`/`EditFile`/`WriteFile`/`Glob`/`Grep`銆?- `hooks.go`锛歱re/post/approval-request 涓夌被 hook 鎸傜偣锛堟彁浜?f5bb856锛夈€?- 鍏跺畠锛歚file_state_cache.go`锛圗ditFile 鍓嶅繀椤?ReadFile锛夈€乣MaxOutputChars=10000`銆乣SkipDirs`銆?
## 6. 鐭涚浘姹囨€伙紙閲嶅啓鏃朵慨姝ｏ級

1. 宸ヤ綔娴佺粨鏋勫彉鏇达細`Sequential(Loop(incident, incident_contract_gate, execution, gate), final_report)`銆?2. rca_agent/strategy_agent/observation_collector 浠ｇ爜浠嶅湪浣?*涓嶅湪缂栨帓涓?*锛坆ootstrap 浠呭垵濮嬪寲 dialogue銆乲nowledge锛夈€?3. 鏂板 incident_contract_gate 鑺傜偣鍙?agentteams/銆乻lash/銆乼oolkit/ 涓変釜鐩綍锛屾枃妗ｆ湭鎻愬強銆?4. Loop 榛樿 3 娆°€乨ialogue 8 宸ュ叿銆乪xecution 6 宸ュ叿閾俱€丷CA 鍏瓧娈佃緭鍑猴細**鏂囨。鍧囨纭紝鏃犻渶鏀瑰姩**銆?5. slash `/ops` 璺敱鏄叆鍙ｅ眰鐨勯噸瑕佽ˉ鍏咃紙褰掑睘鍏ュ彛/API 灞傝寖鍥达級銆?

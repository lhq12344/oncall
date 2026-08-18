# SSE (Server-Sent Events) 鎶€鏈瑙?
> 鏈枃妗ｈ缁嗚В鏋?SSE 鍦?OnCall 椤圭洰涓殑浣滅敤銆佸師鐞嗐€侀€傜敤鎬у強鍏蜂綋瀹炵幇銆?
---

## 涓€銆丼SE 鏄粈涔堬紵

### 1.1 瀹氫箟

**SSE (Server-Sent Events)** 鏄?HTML5 瑙勮寖涓殑涓€绉嶆祻瑙堝櫒鏍囧噯鎶€鏈紝鍏佽鏈嶅姟鍣ㄥ悜瀹㈡埛绔崟鍚戞帹閫佸疄鏃舵暟鎹€?
- **鍗忚**锛氬熀浜?HTTP 鐨勯暱杩炴帴
- **鏍煎紡**锛氭枃鏈牸寮忥紝姣忔潯娑堟伅浠?`data: ` 寮€澶达紝浠?`\n\n` 缁撴潫
- **鐗圭偣**锛氬崟鍚戦€氫俊锛堟湇鍔″櫒 鈫?瀹㈡埛绔級锛岃嚜鍔ㄩ噸杩烇紝绠€鍗曟槗鐢?
### 1.2 涓?WebSocket 鐨勫尯鍒?
| 鐗规€?      | SSE                           | WebSocket                |
| ---------- | ----------------------------- | ------------------------ |
| 閫氫俊鏂瑰悜   | 鍗曞悜锛堟湇鍔″櫒 鈫?瀹㈡埛绔級       | 鍙屽悜                     |
| 鍗忚       | HTTP/1.1+                     | 鐙珛鍗忚                 |
| 杩炴帴鏁?    | 娴忚鍣ㄩ檺鍒?6 涓悓鍩熻繛鎺?      | 鏃犻檺鍒?                  |
| 瀹炵幇澶嶆潅搴?| 绠€鍗曪紙鏂囨湰鏍煎紡锛?             | 杈冨鏉傦紙浜岃繘鍒?甯э級      |
| 閫傜敤鍦烘櫙   | 瀹炴椂閫氱煡銆佹棩蹇楁祦銆丄I 娴佸紡杈撳嚭 | 鍗虫椂閫氳銆佹父鎴忋€佸崗鍚岀紪杈?|

**OnCall 椤圭洰閫夋嫨 SSE 鐨勫師鍥?*锛?
- 鍙渶鏈嶅姟鍣ㄥ悜瀹㈡埛绔帹閫佹祦寮忔暟鎹紙AI 鐢熸垚鍐呭銆佸伐鍏疯皟鐢ㄦ楠ゃ€佷腑鏂簨浠讹級
- 鏃犻渶瀹㈡埛绔悜鏈嶅姟鍣ㄥ彂閫佸ぇ閲忔暟鎹?- 瀹炵幇绠€鍗曪紝娴忚鍣ㄥ師鐢熸敮鎸?
---

## 浜屻€丼SE 鍦?OnCall 椤圭洰涓殑浣滅敤

### 2.1 鏍稿績浣滅敤

SSE 鍦?OnCall 椤圭洰涓壙鎷?**娴佸紡閫氫俊绠￠亾** 鐨勮鑹诧紝瀹炵幇浠ヤ笅鍔熻兘锛?
1. **瀹炴椂娴佸紡杈撳嚭**锛欰I 鐢熸垚鍐呭閫愬瓧鎺ㄩ€侊紝閬垮厤鐢ㄦ埛闀挎椂闂寸瓑寰?2. **宸ュ叿璋冪敤杩涘害灞曠ず**锛氬睍绀?RCA銆丒xecution 绛?Agent 鐨勫伐鍏疯皟鐢ㄦ楠?3. **涓柇浜嬩欢閫氱煡**锛氶珮椋庨櫓鍛戒护瑙﹀彂浜哄伐瀹℃壒鏃讹紝瀹炴椂鎺ㄩ€佷腑鏂簨浠?4. **閿欒瀹炴椂鍙嶉**锛氭墽琛岃繃绋嬩腑鐨勯敊璇疄鏃舵帹閫佸埌鍓嶇
5. **浼氳瘽缁撴潫閫氱煡**锛氭祦寮忎紶杈撶粨鏉熸椂鐨勫畬鎴愪俊鍙?
### 2.2 椤圭洰涓殑 SSE 绔偣

OnCall 椤圭洰鎻愪緵 4 涓?SSE 绔偣锛?
| 绔偣                           | 浣滅敤               | 鍙傛暟                                                                |
| ------------------------------ | ------------------ | ------------------------------------------------------------------- |
| `/api/v1/chat_stream`          | 瀵硅瘽娴佸紡杈撳嚭       | `id` (sessionID), `question`                                        |
| `/api/v1/chat_resume_stream`   | 瀵硅瘽涓柇鎭㈠       | `checkpoint_id`, `interrupt_ids`, `approved`, `resolved`, `comment` |
| `/api/v1/ai_ops_stream`        | 杩愮淮宸ヤ綔娴佹祦寮忚緭鍑?| 鏃犲弬鏁帮紙鍥哄畾璇婃柇锛?                                                 |
| `/api/v1/ai_ops_resume_stream` | 杩愮淮宸ヤ綔娴佷腑鏂仮澶?| `checkpoint_id`, `interrupt_ids`, `approved`, `resolved`, `comment` |

---

## 涓夈€丼SE 涓轰粈涔堥€傜敤浜庡綋鍓嶉」鐩紵

### 3.1 鎶€鏈尮閰嶆€?
| 椤圭洰闇€姹?                                   | SSE 浼樺娍                                  |
| ------------------------------------------- | ----------------------------------------- |
| AI 鐢熸垚鍐呭闇€瑕佸疄鏃跺睍绀?                    | 娴佸紡鎺ㄩ€侊紝閫愬瓧鏄剧ず锛屾彁鍗囩敤鎴蜂綋楠?         |
| 宸ュ叿璋冪敤杩囩▼闇€瑕侀€忔槑鍖?                     | 鍙帹閫?`step` 浜嬩欢锛屽睍绀烘墽琛岃繘搴?         |
| 楂橀闄╂搷浣滈渶瑕佷汉宸ュ鎵?                     | 鍙帹閫?`interrupt` 浜嬩欢锛岃Е鍙戝墠绔鎵瑰崱鐗?|
| 闀挎椂闂磋繍琛岀殑宸ヤ綔娴?                         | 闀胯繛鎺ヤ繚鎸侊紝閬垮厤杞寮€閿€                  |
| 澶氱浜嬩欢绫诲瀷锛坈ontent/step/interrupt/done锛?| 鏂囨湰鏍煎紡鐏垫椿锛屽彲鑷畾涔変簨浠剁被鍨?           |

### 3.2 涓庡叾浠栨柟妗堝姣?
| 鏂规                | 浼樼偣                       | 缂虹偣                   | 閫傜敤鎬?     |
| ------------------- | -------------------------- | ---------------------- | ----------- |
| **SSE锛堝綋鍓嶉€夋嫨锛?* | 绠€鍗曘€佹祻瑙堝櫒鍘熺敓銆佽嚜鍔ㄩ噸杩?| 鍗曞悜閫氫俊               | 鉁?瀹岀編鍖归厤 |
| **WebSocket**       | 鍙屽悜閫氫俊銆佷綆寤惰繜           | 瀹炵幇澶嶆潅銆侀渶瑕侀澶栧崗璁?| 鉂?杩囧害璁捐 |
| **HTTP 杞**       | 绠€鍗?                      | 楂樺欢杩熴€侀珮寮€閿€         | 鉂?浣撻獙宸?  |
| **Long Polling**    | 姣旇疆璇㈤珮鏁?                | 鏈嶅姟鍣ㄨ祫婧愬崰鐢ㄩ珮       | 鉂?涓嶅 SSE |

---

## 鍥涖€丼SE 鍦ㄩ」鐩腑鐨勫叿浣撳疄鐜?
### 4.1 鍚庣瀹炵幇锛圙o + GoFrame锛?
#### 4.1.1 SSE 鍒濆鍖?
```go
// internal/controller/chat/chat_v1.go

func setupSSE(ctx context.Context) (*ghttp.Request, error) {
    r := g.RequestFromCtx(ctx)
    if r == nil {
        return nil, fmt.Errorf("failed to get request from context")
    }
    // 璁剧疆鍝嶅簲澶?    r.Response.Header().Set("Content-Type", "text/event-stream")  // 鍏抽敭锛歁IME 绫诲瀷
    r.Response.Header().Set("Cache-Control", "no-cache")           // 绂佹缂撳瓨
    r.Response.Header().Set("Connection", "keep-alive")            // 淇濇寔杩炴帴
    r.Response.Header().Set("X-Accel-Buffering", "no")             // 绂佺敤 Nginx 缂撳啿
    r.Response.WriteHeader(200)
    r.Response.Flush()
    return r, nil
}
```

#### 4.1.2 鏁版嵁鍐欏叆

```go
// internal/controller/chat/chat_v1.go

func writeSSEData(r *ghttp.Request, data string) {
    if r == nil {
        return
    }
    // 瑙勮寖鍖栨崲琛岀
    data = strings.ReplaceAll(data, "\r\n", "\n")
    data = strings.ReplaceAll(data, "\r", "\n")

    // 鎸夎鍐欏叆锛圫SE 瑙勮寖锛?    lines := strings.Split(data, "\n")
    for _, line := range lines {
        r.Response.Write(fmt.Sprintf("data: %s\n", line))
    }

    // 浜嬩欢缁撴潫绗?    r.Response.Write("\n")

    // 绔嬪嵆鍒锋柊鍒板鎴风
    r.Response.Flush()
}
```

#### 4.1.3 浜嬩欢绫诲瀷瀹氫箟

OnCall 椤圭洰瀹氫箟浜?5 绉?SSE 浜嬩欢绫诲瀷锛?
| 浜嬩欢绫诲瀷      | 鏍煎紡                                                         | 鐢ㄩ€?             |
| ------------- | ------------------------------------------------------------ | ----------------- |
| **content**   | `{"type":"content","content":"..."}`                         | AI 鐢熸垚鐨勬枃鏈唴瀹?|
| **step**      | `{"type":"step","step":N,"content":"..."}`                   | 宸ュ叿璋冪敤姝ラ杩涘害  |
| **interrupt** | `{"type":"interrupt","checkpoint_id":"...","message":"..."}` | 涓柇绛夊緟浜哄伐瀹℃壒  |
| **error**     | `{"type":"error","content":"..."}`                           | 閿欒淇℃伅          |
| **done**      | `{"type":"done"}` 鎴?`[DONE]`                                | 娴佺粨鏉熶俊鍙?       |

#### 4.1.4 瀹屾暣娴佸紡杈撳嚭绀轰緥

```go
// AIOpsStream 鏂规硶锛堢畝鍖栫増锛?
func (c *ControllerV1) AIOpsStream(ctx context.Context, req *v1.AIOpsStreamReq) (*v1.AIOpsStreamRes, error) {
    // 1. 鍒濆鍖?SSE
    r, err := setupSSE(ctx)
    if err != nil {
        return nil, err
    }

    // 2. 杩愯 Runner
    iter := c.opsStreamRunner.Run(ctx, messages, adk.WithCheckPointID(checkpointID))

    // 3. 閬嶅巻浜嬩欢骞舵帹閫?    for {
        event, ok := iter.Next()
        if !ok {
            break
        }

        if event.Err != nil {
            // 鎺ㄩ€侀敊璇簨浠?            writeSSEData(r, fmt.Sprintf("{\"type\":\"error\",\"content\":%q}", event.Err.Error()))
            return nil, nil
        }

        // 鎺ㄩ€?content 浜嬩欢
        if content != "" {
            writeSSEData(r, fmt.Sprintf("{\"type\":\"content\",\"content\":%q}", content))
        }

        // 鎺ㄩ€?step 浜嬩欢锛堝伐鍏疯皟鐢級
        if toolCall != nil {
            writeSSEData(r, fmt.Sprintf("{\"type\":\"step\",\"step\":%d,\"content\":%q}", stepNum, "璋冪敤宸ュ叿: "+call.Function.Name))
        }

        // 鎺ㄩ€?interrupt 浜嬩欢锛堜汉宸ュ鎵癸級
        if interruptInfo != nil {
            payload := buildInterruptPayload(checkpointID, interruptInfo)
            payloadBytes, _ := json.Marshal(payload)
            writeSSEData(r, string(payloadBytes))
        }
    }

    // 4. 鎺ㄩ€佺粨鏉熶簨浠?    writeSSEData(r, "{\"type\":\"done\"}")

    return &v1.AIOpsStreamRes{}, nil
}
```

### 4.2 鍓嶇瀹炵幇锛圧eact + TypeScript锛?
#### 4.2.1 SSE 娑堣垂閫昏緫

```typescript
// frontend/src/services/api.ts

async function streamRequest(url: string, body: any, options: StreamOptions) {
  const { onContent, onStep, onInterrupt, onError, onDone } = options;

  const response = await fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });

  const reader = response.body?.getReader();
  const decoder = new TextDecoder();
  let buffer = "";

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;

    buffer += decoder.decode(value, { stream: true });
    const parts = buffer.split("\n\n"); // SSE 浜嬩欢鍒嗛殧绗?    buffer = parts.pop() || "";

    for (const part of parts) {
      // 鎻愬彇 data 鍐呭
      const dataContent = part
        .split("\n")
        .filter((l) => l.startsWith("data: "))
        .map((l) => l.slice(6))
        .join("\n")
        .trim();

      // 澶勭悊 [DONE] 鏂囨湰鍥為€€
      if (dataContent === "[DONE]") {
        onDone?.();
        return;
      }

      // 瑙ｆ瀽 JSON 浜嬩欢
      try {
        const json = JSON.parse(dataContent);
        switch (json.type) {
          case "content":
            onContent(json.content);
            break;
          case "step":
            onStep?.(json);
            break;
          case "interrupt":
            onInterrupt?.(mapInterruptData(json));
            break;
          case "done":
            onDone?.();
            return;
          case "error":
            onError?.(json.content);
            return;
        }
      } catch (e) {
        // 闈?JSON锛屼綔涓烘櫘閫氭枃鏈鐞?        onContent(dataContent);
      }
    }
  }
}
```

#### 4.2.2 涓柇瀹℃壒鍗＄墖

```typescript
// frontend/src/components/InterruptCard.tsx

// 鐢ㄦ埛鐐瑰嚮瀹℃壒鎸夐挳鍚庯紝鎭㈠ SSE 娴?const handleAction = async (
  actionName: string,
  approved: boolean,
  resolved: boolean,
) => {
  const payload = {
    approved,
    resolved,
    interrupt_ids: interruptIDs,
  };

  // 璋冪敤鎭㈠鎺ュ彛锛屽缓绔嬫柊鐨?SSE 杩炴帴
  if (isOps) {
    await resumeOps(checkpointId, payload, options);
  } else {
    await resumeChat(currentSessionId, checkpointId, payload, options);
  }
};
```

---

## 浜斻€侀潰璇曞洖绛旇瘽鏈?
### 闂 1锛歋SE 鏄粈涔堬紵涓轰粈涔堥€夋嫨 SSE 鑰屼笉鏄?WebSocket锛?
**鍥炵瓟锛?*

> **SSE (Server-Sent Events)** 鏄?HTML5 鏍囧噯涓殑鍗曞悜閫氫俊鎶€鏈紝鍏佽鏈嶅姟鍣ㄥ悜瀹㈡埛绔疄鏃舵帹閫佹暟鎹€傚畠鍩轰簬 HTTP 闀胯繛鎺ワ紝浣跨敤鏂囨湰鏍煎紡锛坄data: \n\n`锛変紶杈撴暟鎹€?>
> **涓轰粈涔堥€夋嫨 SSE 鑰屼笉鏄?WebSocket锛?*
>
> 1. **鍗曞悜閫氫俊瓒冲**锛氭垜浠殑椤圭洰鍙渶鏈嶅姟鍣ㄥ悜瀹㈡埛绔帹閫?AI 鐢熸垚鍐呭銆佸伐鍏疯皟鐢ㄦ楠ゃ€佷腑鏂簨浠讹紝鏃犻渶瀹㈡埛绔悜鏈嶅姟鍣ㄥ彂閫佸ぇ閲忔暟鎹€?> 2. **瀹炵幇绠€鍗?*锛歋SE 鏄祻瑙堝櫒鍘熺敓鏀寔鐨勬爣鍑嗭紝鍓嶇鍙渶 `EventSource` API锛屽悗绔彧闇€璁剧疆鍝嶅簲澶村苟鎸夋牸寮忓啓鍏ユ暟鎹€?> 3. **鑷姩閲嶈繛**锛歋SE 鍐呯疆鑷姩閲嶈繛鏈哄埗锛岀綉缁滀腑鏂悗浼氳嚜鍔ㄦ仮澶嶈繛鎺ャ€?> 4. **娴忚鍣ㄨ繛鎺ラ檺鍒?*锛氭祻瑙堝櫒瀵瑰悓鍩?WebSocket 杩炴帴鏁版棤闄愬埗锛屼絾瀵?SSE 鏈?6 涓繛鎺ラ檺鍒躲€傛垜浠殑椤圭洰姣忎釜浼氳瘽鍙渶 1 涓?SSE 杩炴帴锛屽畬鍏ㄥ鐢ㄣ€?
### 闂 2锛歋SE 鍦?OnCall 椤圭洰涓槸濡備綍浣跨敤鐨勶紵

**鍥炵瓟锛?*

> OnCall 椤圭洰浣跨敤 SSE 瀹炵幇娴佸紡閫氫俊锛屼富瑕佸満鏅寘鎷細
>
> 1. **瀵硅瘽娴佸紡杈撳嚭**锛坄/api/v1/chat_stream`锛夛細
>    - 鎺ㄩ€?AI 鐢熸垚鐨勬枃鏈唴瀹癸紙`content` 浜嬩欢锛?>    - 鎺ㄩ€佸伐鍏疯皟鐢ㄦ楠わ紙`step` 浜嬩欢锛?>    - 鎺ㄩ€佷腑鏂簨浠讹紙`interrupt` 浜嬩欢锛岃Е鍙戜汉宸ュ鎵癸級
> 2. **杩愮淮宸ヤ綔娴佹祦寮忚緭鍑?*锛坄/api/v1/ai_ops_stream`锛夛細
>    - 鎺ㄩ€?RCA Agent 鐨勫垎鏋愯繘搴?>    - 鎺ㄩ€?Execution Agent 鐨勫懡浠ゆ墽琛屾楠?>    - 鎺ㄩ€佹渶缁堟妧鏈姤鍛?> 3. **涓柇鎭㈠**锛坄/api/v1/chat_resume_stream` 鍜?`/api/v1/ai_ops_resume_stream`锛夛細
>    - 鐢ㄦ埛鍦ㄥ墠绔鎵瑰悗锛岄€氳繃 SSE 鎭㈠娴佸紡鎵ц
>    - 缁х画鎺ㄩ€佸悗缁唴瀹?>
> **鍚庣瀹炵幇**锛?>
> - 浣跨敤 GoFrame 鐨?`ghttp.Request` 璁剧疆 SSE 鍝嶅簲澶?> - 浣跨敤 `writeSSEData` 鍑芥暟鎸夋牸寮忓啓鍏ユ暟鎹?> - 浣跨敤 `Flush()` 绔嬪嵆鍒锋柊鍒板鎴风
>
> **鍓嶇瀹炵幇**锛?>
> - 浣跨敤 `fetch` API 鑾峰彇鍝嶅簲娴?> - 浣跨敤 `TextDecoder` 瑙ｇ爜鏁版嵁
> - 鎸?`\n\n` 鍒嗛殧浜嬩欢锛岃В鏋?JSON 鎴栨枃鏈唴瀹?
### 闂 3锛歋SE 濡備綍澶勭悊涓柇鍜屾仮澶嶏紵

**鍥炵瓟锛?*

> **涓柇娴佺▼**锛?>
> 1. 褰?`ExecuteStepTool` 妫€娴嬪埌楂橀闄╁懡浠ゆ椂锛岃皟鐢?`tool.Interrupt()` 鎸傝捣宸ヤ綔娴?> 2. 鍚庣閫氳繃 SSE 鎺ㄩ€?`interrupt` 浜嬩欢锛屽寘鍚?`checkpoint_id` 鍜屼腑鏂俊鎭?> 3. 鍓嶇 `InterruptCard` 缁勪欢娓叉煋瀹℃壒鍗＄墖锛屽睍绀哄緟鎵ц鍛戒护
>
> **鎭㈠娴佺▼**锛?>
> 1. 鐢ㄦ埛鍦ㄥ墠绔偣鍑诲鎵规寜閽紙鍑嗚鎵ц/鎷掔粷/鏍囪宸茶В鍐筹級
> 2. 鍓嶇 POST 鍒版仮澶嶆帴鍙ｏ紙`/chat_resume_stream` 鎴?`/ai_ops_resume_stream`锛?> 3. 鍚庣 `runner.ResumeWithParams()` 鎭㈠鎵ц锛屽伐鍏峰唴 `tool.GetResumeContext()` 鍙栧洖鐢ㄦ埛鍐崇瓥
> 4. 寤虹珛鏂扮殑 SSE 杩炴帴锛岀户缁帹閫佸悗缁唴瀹?>
> **鍏抽敭鐐?*锛?>
> - `checkpoint_id` 鐢ㄤ簬鏍囪瘑涓柇鐐癸紝鏀寔浠绘剰浣嶇疆鎭㈠
> - `interrupt_ids` 鐢ㄤ簬鏍囪瘑鍏蜂綋鐨勪腑鏂笂涓嬫枃
> - 鎭㈠鍚庯紝SSE 缁х画鎺ㄩ€?`content`銆乣step`銆乣interrupt` 绛変簨浠?
### 闂 4锛歋SE 鐨勬€ц兘浼樺寲鏈夊摢浜涳紵

**鍥炵瓟锛?*

> 1. **绂佺敤缂撳啿**锛氳缃?`X-Accel-Buffering: no`锛岀鐢?Nginx 缂撳啿锛岀‘淇濇暟鎹疄鏃舵帹閫?> 2. **绔嬪嵆鍒锋柊**锛氭瘡娆″啓鍏ユ暟鎹悗璋冪敤 `Flush()`锛岄伩鍏嶆暟鎹Н鍘?> 3. **鏂囨湰鏍煎紡**锛歋SE 浣跨敤鏂囨湰鏍煎紡锛屾瘮浜岃繘鍒跺崗璁洿杞婚噺
> 4. **鑷姩閲嶈繛**锛歋SE 鍐呯疆閲嶈繛鏈哄埗锛屾棤闇€棰濆瀹炵幇
> 5. **杩炴帴澶嶇敤**锛氭瘡涓細璇濆彧闇€ 1 涓?SSE 杩炴帴锛岄伩鍏嶈繃澶氳繛鎺ュ紑閿€

---

## 鍏€佷唬鐮佽惤鐐?
| 鍔熻兘               | 鏂囦欢璺緞                                      | 鏍稿績鍑芥暟                                    |
| ------------------ | --------------------------------------------- | ------------------------------------------- |
| SSE 鍒濆鍖?        | `internal/controller/chat/chat_v1.go`         | `setupSSE()`                                |
| 鏁版嵁鍐欏叆           | `internal/controller/chat/chat_v1.go`         | `writeSSEData()`                            |
| 瀵硅瘽娴佸紡杈撳嚭       | `internal/controller/chat/chat_v1.go`         | `ChatStream()`                              |
| 杩愮淮宸ヤ綔娴佹祦寮忚緭鍑?| `internal/controller/chat/chat_v1.go`         | `AIOpsStream()`                             |
| 涓柇鎭㈠           | `internal/controller/chat/chat_v1.go`         | `ChatResumeStream()`, `AIOpsResumeStream()` |
| 鍓嶇 SSE 娑堣垂      | `frontend/src/services/api.ts`              | `streamRequest()`                           |
| 涓柇瀹℃壒鍗＄墖       | `frontend/src/components/InterruptCard.tsx` | `InterruptCard` 缁勪欢                        |

---

## 涓冦€佹€荤粨

SSE 鍦?OnCall 椤圭洰涓壆婕旂潃 **娴佸紡閫氫俊绠￠亾** 鐨勫叧閿鑹诧紝瀹炵幇浜?AI 鐢熸垚鍐呭鐨勫疄鏃跺睍绀恒€佸伐鍏疯皟鐢ㄨ繘搴︾殑閫忔槑鍖栥€侀珮椋庨櫓鎿嶄綔鐨勪汉宸ュ鎵圭瓑鍔熻兘銆傜浉姣?WebSocket锛孲SE 鏇寸畝鍗曘€佹洿閫傚悎鍗曞悜閫氫俊鍦烘櫙锛屾槸娴佸紡 AI 搴旂敤鐨勭悊鎯抽€夋嫨銆?

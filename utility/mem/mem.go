package mem

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sync"
	"time"
	"unicode"

	"go_agent/utility/tokenizer"

	"github.com/cloudwego/eino/schema"
	"github.com/redis/go-redis/v9"
)

/*
商业参数（最大输入 96k、最大输出 8k）
- MaxInputTokens: 96000
- ReserveOutputTokens: 8192
- ReserveToolsDefault: 20000（你暂时不用工具可以传 8000 或 0）
- ReserveUserTokens: 4000（本轮用户输入固定预留；因为 Get 阶段拿不到真实 user tokens）
- SafetyTokens: 2048（协议开销、波动、不可控 token）
- TTL: 2h（滑动过期）
*/
type Config struct {
	MaxInputTokens         int
	ReserveOutputTokens    int
	ReserveToolsDefault    int
	ReserveUserTokens      int
	SafetyTokens           int
	TTL                    time.Duration
	KeepReasoningInContext bool // 默认 false，避免 32k thinking 进入历史
}

var defaultCfg = Config{
	MaxInputTokens:         96000,
	ReserveOutputTokens:    8192,
	ReserveToolsDefault:    20000,
	ReserveUserTokens:      4000,
	SafetyTokens:           2048,
	TTL:                    2 * time.Hour,
	KeepReasoningInContext: false,
}

var (
	rdb    *redis.Client
	cfg    = defaultCfg
	onceMu sync.Mutex
	inited bool
)

// InitRedis：进程启动时调用一次
func InitRedis(client *redis.Client, c *Config) error {
	if client == nil {
		return errors.New("redis client is nil")
	}
	if inited {
		return nil // 已经初始化过了
	}
	onceMu.Lock()
	defer onceMu.Unlock()

	rdb = client
	if c != nil {
		cfg = *c
	}
	inited = true
	return nil
}

// ------------------- 对外 API（替换 SimpleMemoryMap） -------------------

type SimpleMemory struct {
	ID string
}

// GetSimpleMemory：保持你原有使用方式
func GetSimpleMemory(id string) *SimpleMemory {
	return &SimpleMemory{ID: id}
}

// GetMessagesForRequest：包级函数（请求级裁剪在这里做）
func GetMessagesForRequest(ctx context.Context, id string, userMsg *schema.Message, reserveToolsTokens int) ([]*schema.Message, error) {
	return GetSimpleMemory(id).GetMessagesForRequest(ctx, userMsg, reserveToolsTokens)
}

// ------------------- 写入 -------------------

// SetMessages：混合策略写回
// - userTokens：估算为主，但如果提供 promptTokens，会对本轮 userTokens 做校准（scale）
// - assistantTokens：优先用 completionTokens（usage 提供），更精准；没有则估算
//
// promptMsgs：本次真正发给模型的 messages（sys + history + user）
// promptTokens/completionTokens：来自 out.ResponseMeta.Usage
func (m *SimpleMemory) SetMessages(
	ctx context.Context,
	userMsg *schema.Message,
	assistantMsg *schema.Message,
	promptMsgs []*schema.Message,
	promptTokens int,
	completionTokens int,
) error {
	sm := GetSimpleMemory(m.ID)

	if userMsg == nil || assistantMsg == nil {
		return errors.New("mem: userMsg/assistantMsg is nil")
	}

	// 1) user tokens：先估算
	u := sanitizeForContext(userMsg, cfg.KeepReasoningInContext)
	userEst := estimateMessageTokensWithFallback(ctx, u, cfg.KeepReasoningInContext)
	userTok := userEst

	// 2) 如果有 PromptTokens，用它校准本轮 userTok（可选但建议）
	//    scale = promptTokens / estimate(promptMsgs)
	if promptTokens > 0 && len(promptMsgs) > 0 {
		promptEst := 0
		for _, mm := range promptMsgs {
			mm2 := sanitizeForContext(mm, cfg.KeepReasoningInContext)
			promptEst += estimateMessageTokensWithFallback(ctx, mm2, cfg.KeepReasoningInContext)
		}
		if promptEst > 0 {
			scale := float64(promptTokens) / float64(promptEst)
			// 防极端：估算偏差/多模态/toolcalls 可能导致 scale 波动
			if scale < 0.6 {
				scale = 0.6
			} else if scale > 1.6 {
				scale = 1.6
			}
			userTok = int(math.Round(float64(userEst) * scale))
			if userTok < 0 {
				userTok = 0
			}
		}
	}

	// 3) assistant tokens：优先 usage 的 completionTokens（最可靠）
	a := sanitizeForContext(assistantMsg, cfg.KeepReasoningInContext)
	assistantTok := completionTokens
	if assistantTok <= 0 {
		assistantTok = estimateMessageTokensWithFallback(ctx, a, cfg.KeepReasoningInContext) // 兜底
	}

	// 4) 写入（user 新建 turn，assistant 追加到该 turn）
	if err := sm.appendTurnMessage(ctx, u, userTok, int(cfg.TTL.Seconds()), time.Now().Unix()); err != nil {
		return err
	}
	if err := sm.appendTurnMessage(ctx, a, assistantTok, int(cfg.TTL.Seconds()), time.Now().Unix()); err != nil {
		return err
	}

	// 5) 记录本次 usage（可选：方便排查）
	_ = rdb.HSet(ctx, sm.keyMeta(), map[string]any{
		"last_prompt_tokens":     promptTokens,
		"last_completion_tokens": completionTokens,
		"updated_at":             time.Now().Unix(),
	}).Err()

	return nil
}

// ------------------- Get：请求级裁剪（不估算 user token，改为固定预留） -------------------

// GetMessagesForRequest：在 get 时做“请求级裁剪”
// 返回：sys +（裁剪后的 turns）+ userMsg（附加在末尾；不会写入 Redis）
func (m *SimpleMemory) GetMessagesForRequest(ctx context.Context, userMsg *schema.Message, reserveToolsTokens int) ([]*schema.Message, error) {
	if !inited || rdb == nil {
		return nil, errors.New("mem: redis not initialized, call InitRedis first")
	}

	// 1) 读取 sys（永远保留）
	sysMsgs, sysTokens, err := m.loadSystem(ctx)
	if err != nil {
		return nil, err
	}

	// 2) 本轮 user tokens 无法在 Get 阶段从 usage 得到（usage 是 Invoke 之后才有）
	//    因此使用固定预留 ReserveUserTokens（商业常用）
	userTokensReserve := cfg.ReserveUserTokens
	var appendedUser *schema.Message
	if userMsg != nil {
		appendedUser = sanitizeForContext(userMsg, cfg.KeepReasoningInContext)
		if precise := estimateMessageTokensWithFallback(ctx, appendedUser, cfg.KeepReasoningInContext); precise > 0 {
			userTokensReserve = precise
		}
	}

	// 3) tools/RAG 预估（同样是预留）
	if reserveToolsTokens <= 0 {
		reserveToolsTokens = cfg.ReserveToolsDefault
	}

	// 4) 动态 turns budget（不估算 user，使用固定预留）
	turnsBudget := cfg.MaxInputTokens -
		cfg.ReserveOutputTokens -
		reserveToolsTokens -
		userTokensReserve -
		cfg.SafetyTokens -
		sysTokens
	if turnsBudget < 0 {
		turnsBudget = 0
	}

	// 5) 在 Redis 里把 turns 裁剪到 turnsBudget（原子 Lua，按 turn 丢弃）
	if err := m.trimTurnsToBudget(ctx, turnsBudget); err != nil {
		return nil, err
	}

	// 6) 读取 turns 并拼装 messages
	turnMsgs, err := m.loadTurnsMessages(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]*schema.Message, 0, len(sysMsgs)+len(turnMsgs)+1)
	out = append(out, sysMsgs...)
	out = append(out, turnMsgs...)
	if appendedUser != nil {
		out = append(out, appendedUser)
	}

	// 7) 刷新 TTL（滑动过期）
	_ = m.refreshTTL(ctx)

	return out, nil
}

// ------------------- Redis Key 设计 -------------------

func (m *SimpleMemory) keySys() string   { return "aiagent:ctx:" + m.ID + ":sys" }
func (m *SimpleMemory) keyTurns() string { return "aiagent:ctx:" + m.ID + ":turns" }
func (m *SimpleMemory) keyMeta() string  { return "aiagent:ctx:" + m.ID + ":meta" }

// ------------------- 存储结构：System Item / Turn -------------------

// sys 列表中存 {t, msg}，避免 meta 丢失时无法回算（不使用估算）
type storedSysItem struct {
	T   int             `json:"t"`
	Msg *schema.Message `json:"msg"`
}

type storedTurn struct {
	T    int               `json:"t"`    // tokens for this turn
	TS   int64             `json:"ts"`   // last update time
	Msgs []*schema.Message `json:"msgs"` // messages in this turn
}

// ------------------- system 写入/读取 -------------------

func (m *SimpleMemory) loadSystem(ctx context.Context) ([]*schema.Message, int, error) {
	sysTokens, _ := rdb.HGet(ctx, m.keyMeta(), "sys_tokens").Int()

	raw, err := rdb.LRange(ctx, m.keySys(), 0, -1).Result()
	if err != nil && err != redis.Nil {
		return nil, 0, err
	}

	sysMsgs := make([]*schema.Message, 0, len(raw))
	if len(raw) == 0 {
		return sysMsgs, 0, nil
	}

	// 如果 meta 没有 sys_tokens（或为 0），使用 sys 列表中 item.T 回算（不估算）
	if sysTokens <= 0 {
		recalc := 0
		for _, item := range raw {
			var it storedSysItem
			if e := json.Unmarshal([]byte(item), &it); e == nil && it.Msg != nil {
				sysMsgs = append(sysMsgs, sanitizeForContext(it.Msg, cfg.KeepReasoningInContext))
				recalc += it.T
				continue
			}
			// 兼容旧数据：如果存的是 schema.Message（无 token），只能按 0 处理
			var old schema.Message
			if e := json.Unmarshal([]byte(item), &old); e == nil {
				sysMsgs = append(sysMsgs, sanitizeForContext(&old, cfg.KeepReasoningInContext))
			}
		}
		_ = rdb.HSet(ctx, m.keyMeta(), "sys_tokens", recalc).Err()
		return sysMsgs, recalc, nil
	}

	for _, item := range raw {
		var it storedSysItem
		if e := json.Unmarshal([]byte(item), &it); e == nil && it.Msg != nil {
			sysMsgs = append(sysMsgs, sanitizeForContext(it.Msg, cfg.KeepReasoningInContext))
			continue
		}
		// 兼容旧数据
		var old schema.Message
		if e := json.Unmarshal([]byte(item), &old); e == nil {
			sysMsgs = append(sysMsgs, sanitizeForContext(&old, cfg.KeepReasoningInContext))
		}
	}
	return sysMsgs, sysTokens, nil
}

// ------------------- turns 写入：Lua 原子（按 Turn 聚合） -------------------

var luaAppendTurn = redis.NewScript(`
-- KEYS[1] = turns_list_key
-- KEYS[2] = meta_hash_key
-- ARGV[1] = msg_json (schema.Message JSON)
-- ARGV[2] = msg_tokens (int)
-- ARGV[3] = ttl_seconds (int)
-- ARGV[4] = now_ts (int)

local turns = KEYS[1]
local meta  = KEYS[2]

local msgObj = cjson.decode(ARGV[1])
local msgTokens = tonumber(ARGV[2])
local ttl = tonumber(ARGV[3])
local nowTs = ARGV[4]

local turnsTokens = tonumber(redis.call("HGET", meta, "turns_tokens") or "0")
local turnsLen = tonumber(redis.call("LLEN", turns))
local role = msgObj["role"]

if turnsLen == 0 or role == "user" then
  local turn = { t = msgTokens, ts = tonumber(nowTs), msgs = { msgObj } }
  redis.call("RPUSH", turns, cjson.encode(turn))
  turnsTokens = turnsTokens + msgTokens
else
  local last = redis.call("LINDEX", turns, -1)
  if not last then
    local turn = { t = msgTokens, ts = tonumber(nowTs), msgs = { msgObj } }
    redis.call("RPUSH", turns, cjson.encode(turn))
    turnsTokens = turnsTokens + msgTokens
  else
    local turn = cjson.decode(last)
    if not turn["msgs"] then turn["msgs"] = {} end
    table.insert(turn["msgs"], msgObj)
    turn["t"] = (tonumber(turn["t"]) or 0) + msgTokens
    turn["ts"] = tonumber(nowTs)
    redis.call("LSET", turns, -1, cjson.encode(turn))
    turnsTokens = turnsTokens + msgTokens
  end
end

redis.call("HSET", meta, "turns_tokens", turnsTokens)
redis.call("HSET", meta, "updated_at", nowTs)

redis.call("EXPIRE", turns, ttl)
redis.call("EXPIRE", meta, ttl)

return turnsTokens
`)

func (m *SimpleMemory) appendTurnMessage(ctx context.Context, msg *schema.Message, tokens int, ttlSec int, now int64) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	_, err = luaAppendTurn.Run(ctx, rdb,
		[]string{m.keyTurns(), m.keyMeta()},
		string(b),
		tokens,
		ttlSec,
		now,
	).Result()

	_ = rdb.Expire(ctx, m.keySys(), cfg.TTL).Err()
	return err
}

// ------------------- turns 裁剪：Lua 原子（按 turnsBudget 丢弃最旧 Turn） -------------------

var luaTrimTurnsToBudget = redis.NewScript(`
-- KEYS[1] = turns_list_key
-- KEYS[2] = meta_hash_key
-- ARGV[1] = turns_budget (int)
-- ARGV[2] = ttl_seconds (int)
-- ARGV[3] = now_ts (int)

local turns = KEYS[1]
local meta  = KEYS[2]
local budget = tonumber(ARGV[1])
local ttl = tonumber(ARGV[2])
local nowTs = ARGV[3]

local turnsTokens = tonumber(redis.call("HGET", meta, "turns_tokens") or "0")

while turnsTokens > budget do
  local oldTurn = redis.call("LPOP", turns)
  if not oldTurn then
    turnsTokens = 0
    redis.call("HSET", meta, "turns_tokens", 0)
    break
  end
  local ok, ot = pcall(cjson.decode, oldTurn)
  if ok and ot and ot["t"] then
    turnsTokens = turnsTokens - tonumber(ot["t"])
    if turnsTokens < 0 then turnsTokens = 0 end
    redis.call("HSET", meta, "turns_tokens", turnsTokens)
  end
end

redis.call("HSET", meta, "updated_at", nowTs)
redis.call("EXPIRE", turns, ttl)
redis.call("EXPIRE", meta, ttl)

return turnsTokens
`)

func (m *SimpleMemory) trimTurnsToBudget(ctx context.Context, turnsBudget int) error {
	ttlSec := int(cfg.TTL.Seconds())
	now := time.Now().Unix()

	_, err := luaTrimTurnsToBudget.Run(ctx, rdb,
		[]string{m.keyTurns(), m.keyMeta()},
		turnsBudget,
		ttlSec,
		now,
	).Result()
	return err
}

func (m *SimpleMemory) loadTurnsMessages(ctx context.Context) ([]*schema.Message, error) {
	raw, err := rdb.LRange(ctx, m.keyTurns(), 0, -1).Result()
	if err != nil && err != redis.Nil {
		return nil, err
	}
	out := make([]*schema.Message, 0, 64)
	for _, item := range raw {
		var t storedTurn
		if e := json.Unmarshal([]byte(item), &t); e != nil {
			continue
		}
		for _, msg := range t.Msgs {
			out = append(out, sanitizeForContext(msg, cfg.KeepReasoningInContext))
		}
	}
	return out, nil
}

// 刷新 TTL（滑动过期）
func (m *SimpleMemory) refreshTTL(ctx context.Context) error {
	pipe := rdb.Pipeline()
	pipe.Expire(ctx, m.keySys(), cfg.TTL)
	pipe.Expire(ctx, m.keyTurns(), cfg.TTL)
	pipe.Expire(ctx, m.keyMeta(), cfg.TTL)
	_, err := pipe.Exec(ctx)
	return err
}

// ------------------- Message 净化（不涉及 token 估算） -------------------

func sanitizeForContext(m *schema.Message, keepReasoning bool) *schema.Message {
	if m == nil {
		return nil
	}
	cp := *m
	cp.ResponseMeta = nil // 元数据只用于日志/统计，不回灌
	cp.Extra = nil        // any-map 风险大，不回灌
	if !keepReasoning {
		cp.ReasoningContent = ""
	}
	return &cp
}

// ------------------- 估算token -------------------
// estimateMessageTokens 估算一条 schema.Message 在 LLM prompt 中的大致 token 数。
// 设计目标：用于“窗口裁剪/预算”，不追求与 provider tokenizer 完全一致，但要稳定、可解释。
// includeReasoning: 是否把 ReasoningContent 也算进去（一般建议 false）。
func estimateMessageTokens(m *schema.Message, includeReasoning bool) int {
	if m == nil {
		return 0
	}

	// 1) “footprint”只包含可能进入模型上下文、且会显著影响 token 的字段
	//    注意：ResponseMeta / Extra 不应进入上下文，sanitize 已清理，这里也不纳入 footprint。
	type footprint struct {
		Role    schema.RoleType `json:"role"`
		Content string          `json:"content"`

		// 兼容老字段 / 多模态字段（可能很大）
		MultiContent             []schema.ChatMessagePart   `json:"multi_content,omitempty"`
		UserInputMultiContent    []schema.MessageInputPart  `json:"user_input_multi_content,omitempty"`
		AssistantGenMultiContent []schema.MessageOutputPart `json:"assistant_output_multi_content,omitempty"`

		// name 有时会进入 provider 的消息结构（比如 tool name / function name）
		Name string `json:"name,omitempty"`

		// assistant tool calls，和 tool message 的关联字段
		ToolCalls  []schema.ToolCall `json:"tool_calls,omitempty"`
		ToolCallID string            `json:"tool_call_id,omitempty"`
		ToolName   string            `json:"tool_name,omitempty"`

		ReasoningContent string `json:"reasoning_content,omitempty"`
	}

	fp := footprint{
		Role:                     m.Role,
		Content:                  m.Content,
		MultiContent:             m.MultiContent,
		UserInputMultiContent:    m.UserInputMultiContent,
		AssistantGenMultiContent: m.AssistantGenMultiContent,
		Name:                     m.Name,
		ToolCalls:                m.ToolCalls,
		ToolCallID:               m.ToolCallID,
		ToolName:                 m.ToolName,
	}
	if includeReasoning {
		fp.ReasoningContent = m.ReasoningContent
	}

	// 2) 将 footprint 序列化为 JSON 字符串，作为“结构+内容”的统一载体
	//    JSON 的 key/标点会带来额外 token，这反而符合真实 prompt 的结构开销趋势。
	b, _ := json.Marshal(fp)

	// 3) 结构开销：role、分隔符、消息边界等
	//    不同 provider 不同，这里取一个保守常数；你也可以按经验调参。
	const overhead = 8

	return overhead + estimateTextTokens(string(b))
}

// estimateMessageTokensWithFallback 优先使用 DeepSeek Tokenization 精确计算，失败时回退到本地估算。
func estimateMessageTokensWithFallback(ctx context.Context, m *schema.Message, includeReasoning bool) int {
	if m == nil {
		return 0
	}

	if tokens, err := tokenizer.CountMessageTokens(ctx, m, includeReasoning); err == nil && tokens > 0 {
		return tokens
	}

	return estimateMessageTokens(m, includeReasoning)
}

// estimateTextTokens：对一段文本（通常是 JSON）做 token 粗估算。
// 经验上：
// - ASCII 字符（英文/数字/标点）≈ 0.25~0.35 token/char（取 0.30）
// - CJK 汉字 ≈ 0.50~0.70 token/char（取 0.60）
// - 其它 unicode（emoji、特殊符号）更“贵”，取 1.00 token/char
func estimateTextTokens(s string) int {
	if s == "" {
		return 0
	}

	var ascii, han, other int
	for _, r := range s {
		switch {
		case r <= 0x7F:
			ascii++
		case unicode.Is(unicode.Han, r):
			han++
		default:
			other++
		}
	}

	est := 0.30*float64(ascii) + 0.60*float64(han) + 1.00*float64(other)

	// 向上取整，避免低估导致超窗
	return int(math.Ceil(est))
}

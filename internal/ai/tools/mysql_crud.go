package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	db "go_agent/utility/mysql"
	"gorm.io/gorm"
)

type MysqlCrudInput struct {
	OperateType string `json:"operate_type"` // "query" | "exec"
	SQL         string `json:"sql"`
	// Approved 必须由服务端控制（例如前端点确认后才置 true），不要让模型自行生成
	Approved bool `json:"approved"`
}

type MysqlToolConfig struct {
	DB              *gorm.DB
	ReadOnly        bool // true: 只允许 SELECT
	RequireApproval bool // true: exec 需要 Approved=true
	MaxQueryRows    int  // query 行数上限（可选）
	QueryTimeout    time.Duration
}

func NewMysqlCrudTool() tool.InvokableTool {
	cfg := MysqlToolConfig{
		DB:              db.GlobalMySQL,
		ReadOnly:        true,
		RequireApproval: true,
		MaxQueryRows:    200,
		QueryTimeout:    8 * time.Second,
	}

	if cfg.DB == nil {
		panic("MysqlToolConfig.DB is nil")
	}
	if cfg.MaxQueryRows <= 0 {
		cfg.MaxQueryRows = 200
	}
	if cfg.QueryTimeout <= 0 {
		cfg.QueryTimeout = 8 * time.Second
	}

	t, err := utils.InferOptionableTool(
		"mysql_crud",
		`Execute SQL against MySQL. 
- Use operate_type="query" for SELECT.
- Use operate_type="exec" for INSERT/UPDATE/DELETE (may require approval).
Return JSON results.`,
		func(ctx context.Context, input *MysqlCrudInput, opts ...tool.Option) (string, error) {
			if input == nil {
				return "", errors.New("nil input")
			}

			sqlText := strings.TrimSpace(input.SQL)
			if sqlText == "" {
				return "", errors.New("empty sql")
			}

			// 1) 基本安全校验
			if err := validateSQL(sqlText, input.OperateType, cfg.ReadOnly); err != nil {
				return "", err
			}

			// 2) 执行类型
			switch strings.ToLower(strings.TrimSpace(input.OperateType)) {
			case "query":
				// 给 query 加超时
				qctx, cancel := context.WithTimeout(ctx, cfg.QueryTimeout)
				defer cancel()

				rows, err := cfg.DB.WithContext(qctx).Raw(sqlText).Rows()
				if err != nil {
					return "", err
				}
				defer rows.Close()

				result, err := rowsToJSON(rows, cfg.MaxQueryRows)
				if err != nil {
					return "", err
				}
				return string(result), nil

			case "exec":
				if cfg.ReadOnly {
					return "", errors.New("tool is in read-only mode; exec is disabled")
				}
				if cfg.RequireApproval && !input.Approved {
					// 这里返回一个“需要审批”的明确提示，让上层可以走人工确认流程
					return `{"error":"approval_required","message":"exec requires Approved=true from server-side confirmation"}`, nil
				}

				res := cfg.DB.WithContext(ctx).Exec(sqlText)
				if res.Error != nil {
					return "", res.Error
				}

				out := map[string]any{
					"rows_affected": res.RowsAffected,
				}
				b, _ := json.Marshal(out)
				return string(b), nil

			default:
				return "", fmt.Errorf("invalid operate_type=%q, must be query|exec", input.OperateType)
			}
		},
	)
	if err != nil {
		panic(err)
	}
	return t
}

// ---------------- helpers ----------------

// 防多语句、危险关键字、以及 query/exec 类型基本约束
func validateSQL(sqlText string, operateType string, readOnly bool) error {
	low := strings.ToLower(strings.TrimSpace(sqlText))

	// 1) 禁止多语句（非常重要）
	// 允许末尾一个分号（可选），但不允许中间出现分号
	if strings.Count(low, ";") > 0 {
		trim := strings.TrimSuffix(low, ";")
		if strings.Contains(trim, ";") {
			return errors.New("multi-statement SQL is not allowed")
		}
	}

	// 2) 禁止高风险关键字
	// 你可以按业务再加：load_file, outfile, dumpfile, grant, revoke, alter, drop, truncate...
	danger := []string{
		" drop ", " truncate ", " alter ", " grant ", " revoke ",
		" create ", " rename ", " shutdown ", " load_file", " into outfile", " into dumpfile",
	}
	padded := " " + low + " "
	for _, kw := range danger {
		if strings.Contains(padded, kw) {
			return fmt.Errorf("dangerous keyword detected: %s", strings.TrimSpace(kw))
		}
	}

	op := strings.ToLower(strings.TrimSpace(operateType))
	if op == "" {
		// 默认当 query（更安全）
		op = "query"
	}

	// 3) 只读模式
	if readOnly && op != "query" {
		return errors.New("read-only mode: only query is allowed")
	}

	// 4) 约束 query 只能 SELECT
	if op == "query" {
		if !strings.HasPrefix(strings.TrimSpace(low), "select") {
			return errors.New(`operate_type="query" only allows SELECT`)
		}
		return nil
	}

	// 5) exec 约束：只允许 insert/update/delete（可扩展）
	if op == "exec" {
		ok := strings.HasPrefix(low, "insert") || strings.HasPrefix(low, "update") || strings.HasPrefix(low, "delete")
		if !ok {
			return errors.New(`operate_type="exec" only allows INSERT/UPDATE/DELETE`)
		}
		// 强约束：update/delete 必须带 where（避免全表）
		if strings.HasPrefix(low, "update") || strings.HasPrefix(low, "delete") {
			whereRe := regexp.MustCompile(`(?i)\bwhere\b`)
			if !whereRe.MatchString(low) {
				return errors.New("UPDATE/DELETE must include WHERE clause")
			}
		}
		return nil
	}

	return fmt.Errorf("invalid operate_type=%q", operateType)
}

// 将 sql.Rows 转为 []map[string]any 的 JSON（带行数上限）
func rowsToJSON(rows *sql.Rows, maxRows int) ([]byte, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	out := make([]map[string]any, 0, 32)

	for rows.Next() {
		if maxRows > 0 && len(out) >= maxRows {
			break
		}

		// 每列一个目的地
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}

		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}

		row := make(map[string]any, len(cols))
		for i, c := range cols {
			v := vals[i]
			// []byte 常见于 VARCHAR/TEXT/JSON
			if b, ok := v.([]byte); ok {
				row[c] = string(b)
			} else {
				row[c] = v
			}
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return json.Marshal(out)
}

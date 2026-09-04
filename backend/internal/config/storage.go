package config

import (
	"fmt"
	"strings"
	"time"
)

type Storage struct {
	Redis         Redis
	Elasticsearch Elasticsearch
	Milvus        Milvus
}

type Redis struct {
	Required    bool
	Addr        string
	Password    string
	DB          int
	DialTimeout time.Duration
}

type Elasticsearch struct {
	Required  bool
	Addresses []string
	Username  string
	Password  string
	CloudID   string
	APIKey    string
	Timeout   time.Duration
	TLSSkip   bool
}

type Milvus struct {
	Required              bool
	Address               string
	Database              string
	Collection            string
	KnowledgeV2Collection string
	OpsV2Collection       string
	Timeout               time.Duration
	AutoCreateCollection  bool
}

func DefaultMilvus() Milvus {
	return Milvus{
		Required:              false,
		Address:               "localhost:31953",
		Database:              "agent",
		Collection:            "biz",
		KnowledgeV2Collection: "biz_v2",
		OpsV2Collection:       "ops_cases_v2",
		Timeout:               8 * time.Second,
		AutoCreateCollection:  true,
	}
}

func (s Storage) Validate() error {
	if s.Redis.Required && strings.TrimSpace(s.Redis.Addr) == "" {
		return fmt.Errorf("redis address is required when redis.required=true")
	}
	if s.Redis.DialTimeout < 0 {
		return fmt.Errorf("redis dial timeout must not be negative")
	}
	if s.Elasticsearch.Required && len(s.Elasticsearch.Addresses) == 0 && strings.TrimSpace(s.Elasticsearch.CloudID) == "" {
		return fmt.Errorf("elasticsearch address or cloud id is required when elasticsearch.required=true")
	}
	if s.Elasticsearch.Timeout < 0 {
		return fmt.Errorf("elasticsearch timeout must not be negative")
	}
	if s.Milvus.Required && strings.TrimSpace(s.Milvus.Address) == "" {
		return fmt.Errorf("milvus address is required when milvus.required=true")
	}
	if s.Milvus.Timeout < 0 {
		return fmt.Errorf("milvus timeout must not be negative")
	}
	return nil
}

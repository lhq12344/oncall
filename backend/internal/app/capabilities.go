package app

func (c Capabilities) OptionalDependencies() map[string]bool {
	return map[string]bool{
		"redis":         c.RedisOptional,
		"elasticsearch": c.ElasticsearchOptional,
		"milvus":        c.MilvusOptional,
		"trace":         c.TraceOptional,
	}
}

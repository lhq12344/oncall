package quality

func AllowsSend(result EvaluationResult) bool { return result.Status == Pass }

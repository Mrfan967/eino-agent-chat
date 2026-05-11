package config

type ModelConf struct {
	APIKey  string
	BaseURL string
	Model   string
}

type ModelsConf struct {
	KimiK2 ModelConf
	GLM4   ModelConf
}

type PromptConf struct {
	Path string
}

type RpcClientConf struct {
	Endpoints []string
}

type Config struct {
	Name         string
	ListenOn     string
	Models       ModelsConf
	PromptConfig PromptConf
	RAGRpc       RpcClientConf
	SessionRpc   RpcClientConf
}

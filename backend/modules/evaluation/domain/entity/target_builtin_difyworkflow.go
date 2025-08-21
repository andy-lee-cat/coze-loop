package entity

// DifyWorkflow 代表一个被用作评测目标的Dify Workflow配置。
type DifyWorkflow struct {
	// Name 是用户为这个Dify评测目标配置起的名字，方便在UI上识别。
	// 例如："我的订单处理工作流"
	Name string `json:"name"`

	// APIKey 是调用此Dify Workflow所必需的凭证。
	// 为了安全，我们不在JSON序列化中直接暴露它。
	APIKey string `json:"-"` // 使用 `json:"-"` 避免在API响应中意外泄露

	// Description 是对这个评测目标的可选描述。
	Description string `json:"description"`

	// 【可以考虑增加的字段】
	// DifyHost 如果你想让每个评测目标可以指向不同的Dify实例（比如测试环境和生产环境），
	// 可以在这里存储Host。如果所有目标都指向同一个Host，则此字段非必需，可以放在全局配置里。
	// DifyHost string `json:"dify_host"`
}

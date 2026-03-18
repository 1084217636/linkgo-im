最重要的部分，存放不希望被外部引用的核心业务逻辑 
JWT 鉴权体系：

采用 golang-jwt 实现有状态/无状态校验。

鉴权中间件通过 gin.HandlerFunc 封装，支持从 HTTP Header 和 WebSocket URL Query 双向提取凭证。

校验通过后通过 gin.Context 传递 user_id 至后续业务链。

go get github.com/golang-jwt/jwt/v5 和python pip相似
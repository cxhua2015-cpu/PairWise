# 控制面依赖图 173 specification

原子批次支持 AddNode/DeleteNode/AddEdge/DeleteEdge。加边必须阻止有向环；删除节点同时删除关联边。最终节点/边容量只在批次末检查，失败整体回滚。Reachable 使用当前一致快照，Snapshot 对节点和边稳定排序。

名称/键仅允许非空 ASCII 小写字母、数字、连字符和下划线，并受 Options 字节上限约束。Options 中的容量和长度上限必须为正。批次必须先完整结构校验，再读取状态；未知 kind 或额外字段返回 `ErrInvalidInput`。非空成功批次 generation 只增加一次，空批次不变。所有公开方法并发安全，返回切片与内部状态隔离。具体公开结构、错误值和边界以包内类型及契约测试为准。

# 就绪优先队列 245 specification

队列使用显式非负单调时间，Apply 原子顺序执行 Enqueue/Cancel，Enqueue 分配 revision，最终容量只在末尾检查。Pop 选择 ReadyAt <= now 的任务，按 Priority 降序、ReadyAt 升序、ID 升序并原子删除。失败回滚时间、状态和 revision。

名称/键仅允许非空 ASCII 小写字母、数字、连字符和下划线，并受 Options 字节上限约束。Options 中的容量和长度上限必须为正。批次必须先完整结构校验，再读取状态；未知 kind 或额外字段返回 `ErrInvalidInput`。非空成功批次 generation 只增加一次，空批次不变。所有公开方法并发安全，返回切片与内部状态隔离。具体公开结构、错误值和边界以包内类型及契约测试为准。

# 元数据目录 436 specification

原子批次按输入顺序执行 Put/Delete；Put 分配连续 revision，Delete 不分配。完整结构校验先于状态读取，最终记录数与 Value 总字节容量只在批次末检查。失败回滚全部状态、generation 和 revision。Get/Snapshot 深拷贝 Value，Snapshot 按名称排序。

名称/键仅允许非空 ASCII 小写字母、数字、连字符和下划线，并受 Options 字节上限约束。Options 中的容量和长度上限必须为正。批次必须先完整结构校验，再读取状态；未知 kind 或额外字段返回 `ErrInvalidInput`。非空成功批次 generation 只增加一次，空批次不变。所有公开方法并发安全，返回切片与内部状态隔离。具体公开结构、错误值和边界以包内类型及契约测试为准。

## 多文件联动要求

除核心事务外，还必须实现三个相互依赖的组件：`validation.go` 中的无副作用批次预检、`stats.go` 中的线性一致状态统计，以及 `clone.go` 中保留逻辑时钟且完全隔离所有权的深拷贝。`Apply` 与 `ValidateBatch` 必须共享同一套结构语义；`Stats` 和 `Clone` 必须与并发事务状态保持一致。此任务要求至少联动四个实现文件，不能将实现收缩到单个文件。


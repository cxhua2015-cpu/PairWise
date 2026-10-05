# 余额账本 422 specification

原子批次按输入顺序执行 Add/Set/Delete；Add/Set 分配连续 revision。必须在算术前检测 int64 溢出并执行绝对值上限，最终账户容量仅在批次末检查。失败整体回滚。Top 按数值降序、名称升序，Snapshot 按名称排序。

名称/键仅允许非空 ASCII 小写字母、数字、连字符和下划线，并受 Options 字节上限约束。Options 中的容量和长度上限必须为正。批次必须先完整结构校验，再读取状态；未知 kind 或额外字段返回 `ErrInvalidInput`。非空成功批次 generation 只增加一次，空批次不变。所有公开方法并发安全，返回切片与内部状态隔离。具体公开结构、错误值和边界以包内类型及契约测试为准。

## 多文件联动要求

除核心事务外，还必须实现四个相互依赖的组件：`validation.go` 中的无副作用批次预检、`stats.go` 中的线性一致状态统计、`clone.go` 中保留逻辑时钟且完全隔离所有权的深拷贝，以及 `preview.go` 中的事务预演。`Preview` 必须在一次线性化快照上模拟 `Apply`，返回与该快照实际提交完全一致的 `Result`、候选 `Snapshot` 和候选 `Stats`，但不得改变原对象的状态、generation、revision 或逻辑时间；失败时必须返回与 `Apply` 相同的错误并保持全部返回值为零值。所有返回切片都必须与原对象及候选状态隔离。`Apply` 与 `ValidateBatch` 必须共享结构语义，`Stats`、`Clone` 和 `Preview` 必须保持同一状态模型。此任务要求至少联动五个实现文件，不能将实现收缩到单个文件。


# 余额账本 427 specification

原子批次按输入顺序执行 Add/Set/Delete；Add/Set 分配连续 revision。必须在算术前检测 int64 溢出并执行绝对值上限，最终账户容量仅在批次末检查。失败整体回滚。Top 按数值降序、名称升序，Snapshot 按名称排序。

名称/键仅允许非空 ASCII 小写字母、数字、连字符和下划线，并受 Options 字节上限约束。Options 中的容量和长度上限必须为正。批次必须先完整结构校验，再读取状态；未知 kind 或额外字段返回 `ErrInvalidInput`。非空成功批次 generation 只增加一次，空批次不变。所有公开方法并发安全，返回切片与内部状态隔离。具体公开结构、错误值和边界以包内类型及契约测试为准。

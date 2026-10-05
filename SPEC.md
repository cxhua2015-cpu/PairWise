# 到期状态表 309 specification

表使用显式非负单调时间。Apply 先结构校验再检查时间，在候选状态先删除 ExpiresAt <= Now 的条目，再顺序执行 Put/Touch/Delete；Put/Touch 分配 revision。最终容量失败或任何错误须连同淘汰、时间和 revision 一起回滚。Expire 使用相同闭区间边界。

名称/键仅允许非空 ASCII 小写字母、数字、连字符和下划线，并受 Options 字节上限约束。Options 中的容量和长度上限必须为正。批次必须先完整结构校验，再读取状态；未知 kind 或额外字段返回 `ErrInvalidInput`。非空成功批次 generation 只增加一次，空批次不变。所有公开方法并发安全，返回切片与内部状态隔离。具体公开结构、错误值和边界以包内类型及契约测试为准。

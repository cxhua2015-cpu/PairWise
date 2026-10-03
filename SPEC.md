# netpolicy contract

实现使用 Go 1.22+ 标准库，并保留 `netpolicy/netpolicy.go` 中的公开 API、常量和错误值。

## Options 与字段边界

- `MaxRules`：1..10000；`MaxValueBytes`：1..64 MiB，只统计当前规则 Value 的字节数。
- ID 长度 1..64 字节，只允许 ASCII 字母、数字、点、下划线和连字符。
- Prefix 使用 `net/netip.ParsePrefix` 接受的无 zone IPv4/IPv6 CIDR。加入时必须 `Unmap` IPv4-mapped IPv6 地址并 `Masked` 清除主机位，Snapshot/Decision 返回规范字符串。
- Protocol 仅允许 `ProtocolAny`、`ProtocolTCP`、`ProtocolUDP`。包匹配时只允许 TCP/UDP。
- Any 规则必须使用端口 `0..0`；TCP/UDP 规则要求 `PortStart <= PortEnd`。端口 0 是合法端口。
- Priority 范围 -1000..1000；Action 仅允许 Allow/Deny；单个 Value 最多 1 MiB。

## Apply 与 ApplyBatch

- `Apply` 必须等价于单元素 `ApplyBatch`。
- 空批成功并返回当前 generation，不改变状态。
- 校验分两阶段：先按输入顺序完成所有 change 的结构校验，再在候选副本上按输入顺序执行语义操作。结构错误优先于任何重复/缺失错误。
- Add 要求 `Change.ID` 为空并完整校验 `Rule`；Delete 要求 `Change.Rule.ID` 为空且 `Change.ID` 合法。未知 ChangeType 返回 `ErrInvalidChange`。
- Add 已存在 ID 返回 `ErrDuplicateID`；Delete 不存在 ID 返回 `ErrNotFound`。同一批可 Delete 后 Add 同一 ID。
- 全部操作成功后才检查最终 `MaxRules` 和 `MaxValueBytes`。失败批次不得改变规则、generation、容量或输入数据。
- 非空成功批次 generation 恰好加一，即使最终规则内容与批次前相同。

## Match 与稳定优先级

- address 必须是无 zone 的 `netip.ParseAddr` 字符串；IPv4-mapped IPv6 必须 `Unmap` 后按 IPv4 匹配。非法地址返回 `ErrInvalidAddress`。
- protocol 只允许 TCP/UDP，否则 `ErrInvalidProtocol`。
- 候选规则需前缀包含地址，且协议为 Any 或等于请求协议；具体协议还需端口位于闭区间内。
- 候选按以下键依次择优：前缀位数更多；具体协议优于 Any；端口区间宽度更小；Priority 更高；Deny 优于 Allow；ID 字节序更小。Any 的端口宽度视为 65536。
- 无候选成功返回 `Decision{Found:false, Generation:当前值}`。
- Decision 中的 Value 必须深拷贝。

## Snapshot、排序和所有权

- Snapshot Rules 按规范 Prefix 的地址族（IPv4 在前）、网络地址、前缀位数降序、Protocol、PortStart、PortEnd、Priority 降序、Action、ID 排序。
- 所有输入 Value 以及 Decision/Snapshot 返回 Value 必须与内部状态及彼此隔离。
- 所有公开方法可被多个 goroutine 并发调用并通过 race detector。

## 错误包装

操作级错误可以包装公开 sentinel，但 `errors.Is` 必须成立。一次失败返回规定校验顺序遇到的第一个错误。

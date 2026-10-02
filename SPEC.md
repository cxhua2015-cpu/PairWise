# 事件时间双流关联器：验收合同 v1

## 目标与 API

实现 `join` 包，用于关联乱序到达的左右两路事件。项目使用 Go 1.22 或更高版本且仅依赖标准库。

公开 API 已在 `join/joiner.go` 中定义：

- `New(Options) (*Joiner, error)`
- `Apply(Update) (Outcome, error)`
- `ApplyBatch([]Update) ([]Outcome, error)`
- `Snapshot() Snapshot`

必须保留现有公开名称、字段和类型。骨架错误支持 `errors.Is`；实现可以增加非公开类型、字段和文件。

## 参数与通用验证

`Options.Window`、`MaxEvents`、`MaxBytes` 必须大于零，且 `Window <= 10^12`，否则 `ErrInvalid`。两个水位线初始均为 0。

每个 `Update` 必须恰好包含一个非 nil 指针：`Event` 或 `Watermark`，否则 `ErrInvalid`。事件必须满足：

- `Side` 是 `Left` 或 `Right`；
- `Key` 和 `ID` 非空；
- `0 <= Time <= 10^12`；
- Payload 可以为空。

水位线必须满足 Side 合法且 `0 <= Time <= 10^12`。水位线小于该侧当前水位线返回 `ErrTime`；等于当前值是合法的空进展。

任何失败的 `Apply` 或 `ApplyBatch` 必须完全无副作用。发生错误时 `Apply` 返回零值 `Outcome`，`ApplyBatch` 返回 nil slice。不得暴露或保留调用方可变 slice。

## 事件时间、水位线与迟到

水位线表示对应一侧以后不会再出现时间戳小于水位线的事件。事件满足下列任一条件时返回 `ErrLate`：

1. 事件时间小于同侧当前水位线；
2. 对侧当前水位线严格大于 `event.Time + Window`。

第二项加法必须避免整数溢出。等于边界不是迟到，例如右水位线等于 `left.Time + Window` 时仍可接收左事件。

更新某一侧水位线后，立即过期对侧所有满足 `event.Time + Window < 新水位线` 的事件。边界相等时保留。每次水位线更新的 `Outcome.Expired` 只包含该次新过期的事件，按 `Time`、`Key`、`ID` 升序排列。事件更新的 `Expired` 为空。

## 匹配与确定性

新事件与当前仍保留的全部对侧事件匹配，当且仅当 Key 相同且两者时间差的绝对值不大于 Window。不得用可能溢出的减法实现时间差判断。

每一对事件只在这对事件中的后到者首次被接收时输出一次。事件更新的 `Outcome.Matches` 按对侧事件的 `Time`、`ID` 升序排列；水位线更新的 `Matches` 为空。`Match` 中 Left/Right 字段必须始终对应各自 Side，与到达顺序无关。所有字符串按 Go 字节序比较。

实现必须复制输入 Payload。返回的 `Match.Left`、`Match.Right` 也必须与内部状态隔离；调用方修改输入或既往返回值，不能改变未来结果或快照。

## 活跃 ID、重复与冲突

ID 的作用域是 Side，即 `(Side, ID)` 在当前活跃集合内唯一，Key 不属于 ID 作用域。

- 若同 Side、同 ID 的活跃事件与输入的 Key、Time、Payload 完全相同，则输入是幂等重复：成功返回空 `Outcome`，不重新匹配，也不增加容量。
- 若同 Side、同 ID 的活跃事件有任一上述字段不同，则返回 `ErrConflict`。
- 事件一旦由水位线过期，其 ID 不保留墓碑，可以用于新的事件。

冲突检查先于迟到检查：只要活跃 ID 冲突就返回 `ErrConflict`。完全相同的活跃重复也先于迟到检查，因此即使水位线后来推进，它仍成功返回空结果，直到该事件真正过期。

## 容量与批量事务

`Snapshot.Count` 是当前活跃事件数。`Snapshot.Bytes` 是所有活跃事件的 `len(Key)+len(ID)+len(Payload)` 之和，按 Go 字符串和 byte slice 的字节长度计算。`MaxEvents` 和 `MaxBytes` 限制事务最终状态；等于上限合法。

`ApplyBatch` 按输入顺序在隔离事务中执行，每个 `Outcome` 对应同位置输入。批次内后续更新可以观察先前更新的结果，包括匹配、水位线与过期。任一步验证失败、迟到、冲突，或最终状态超过容量，整个批次回滚。

容量只在整批处理完毕后检查。因此允许批次中途暂时超限，随后通过水位线更新过期事件，使最终状态回到预算内。`Apply` 与单元素 `ApplyBatch` 语义完全一致。空批次成功返回长度为零的非 nil slice，状态不变。

已经在失败事务中准备好的 Matches/Expired 不得泄漏给调用方，也不得造成以后重试丢失输出。

## 快照与并发

`Snapshot.Events` 包含全部活跃事件，但不包含 Payload，只记录按定义计算的 Bytes。排序键依次为 `Side`、`Time`、`Key`、`ID` 升序。Snapshot 的 Count/Bytes 必须与 Events 一致，水位线取同一次线性化时刻的值；返回 slice 不得与内部存储共享。

同一 Joiner 的所有公开方法必须支持多个 goroutine 并发调用，行为可线性化且无数据竞争。允许使用互斥锁串行化事务。禁止读取墙钟、访问网络或启动后台 goroutine。

实现应按 Key 和 Side 组织活跃事件，避免每次匹配扫描无关 Key；水位线过期可以扫描活跃集合。README 必须如实说明索引、事务复制、容量统计、排序以及各公开操作的实际时间/空间复杂度。

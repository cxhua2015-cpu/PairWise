# reservation contract

## 边界

- `MaxResources` 1..10000，`MaxReservations` 1..1,000,000，`MaxValueBytes` 1..64 MiB。
- ID 与 Resource 长度 1..64 字节，只允许 ASCII 字母、数字、点、下划线、连字符。
- `Start < End`，二者可使用任意 int64 值；区间为半开 `[Start,End)`。单个 Value 最多 1 MiB。

## 批量事务与冲突

- `Apply` 等价于单元素 `ApplyBatch`；空批返回当前 generation。
- 先按输入顺序完成全部 change 结构校验，再在候选副本按输入顺序执行语义操作。Add 要求 `Change.ID` 为空并完整校验 Reservation；Delete 要求 `Change.Reservation.ID` 为空且 `Change.ID` 合法。
- Add 已存在 ID 返回 `ErrDuplicate`；Delete 不存在 ID 返回 `ErrNotFound`；批内可 Delete 后 Add 同一 ID。
- 全部语义操作完成后，才检查最终状态：资源数、预约数、Value 总字节和同一资源内重叠。区间相邻不冲突；重叠返回 `ErrConflict`。检查失败零副作用。
- 非空成功批 generation 加一，即使最终内容与之前相同。

## At、Scan 与 Snapshot

- `At(resource,time)` 校验 resource；返回覆盖 time 的预约和当前 generation，若无则 `Found:false`。`time==End` 不覆盖。
- `Scan(resource,from,to,limit)` 要求合法 resource、`from < to`、limit 1..1000；返回与 `[from,to)` 重叠的预约，按 Start、End、ID 排序；未知资源成功返回空结果。
- Snapshot Resources 按 Resource 升序；每个资源内 Reservations 按 Start、End、ID 排序。
- 所有输入 Value 以及 At/Scan/Snapshot 返回 Value 必须与内部状态及彼此隔离。所有公开方法并发安全并通过 race detector。
- 操作级错误可以包装 sentinel，但 `errors.Is` 必须成立。

# reservation

并发安全的内存区间预约账本（Go 1.22+，仅标准库）。完整合同见 `SPEC.md`。

## 数据模型与索引

- 每个资源的预约存储在按 `(Start, End, ID)` 排序的切片中，配合二分查找定位；
  另有一张 `id -> entry` 哈希表用于 O(1) 按 ID 定位。
- 插入/删除在有序切片上二分定位后移动元素：O(log n) 查找 + O(n) 移动。
- 区间为**半开 `[Start, End)`**：`At(t)` 命中当且仅当 `Start <= t < End`；
  相邻区间（前者的 `End` 等于后者的 `Start`）不算冲突。`Start`、`End` 支持
  任意 int64 值（含 `math.MinInt64`/`MaxInt64`），比较全程无加减溢出风险。

## 事务（Apply / ApplyBatch）

- `Apply` 等价于单元素 `ApplyBatch`；空批返回当前 generation，不推进。
- 先按输入顺序完成全部结构校验（类型、ID/Resource 字符集与长度、`Start<End`、
  单 Value ≤ 1 MiB），任一失败即返回，零副作用。
- 校验通过后在整个状态的深拷贝候选上按输入顺序执行语义操作
  （Add 要求 ID 不存在，Delete 要求 ID 存在；批内允许 Delete 后重 Add 同一 ID）。
- 语义完成后才对**最终状态**检查容量（资源数、预约数、Value 总字节）与
  同资源重叠；失败整体回滚，committed 状态与 generation 均不变。
- 成功的非空批次 generation 恰好加一，即使最终内容与之前相同。

## 冲突检测与容量

- 冲突只在提交前对候选最终状态检查：每个资源的有序切片做一次线性相邻扫描，
  `cur.Start < prev.End` 即重叠，返回 `ErrConflict`；复杂度 O(n)。
- 容量检查（`ErrCapacity`）同样只针对最终状态，因此批内"先删后加"可以
  替换预约而不触发瞬时超限。

## 查询

- `At(resource, t)`：校验 resource 后二分查找第一个 `End > t` 的条目，
  命中条件 `Start <= t`；O(log n)。返回当前 generation。
- `Scan(resource, from, to, limit)`：要求 `from < to` 且 `limit ∈ [1,1000]`；
  返回与 `[from,to)` 重叠的预约，按 `(Start,End,ID)` 排序，最多 limit 条；
  未知资源返回空结果而非错误。O(log n + k)。
- `Snapshot()`：Resources 按名称升序，每个资源内按 `(Start,End,ID)` 排序，
  并附带 generation、预约总数与 Value 总字节。O(n)。

## 所有权与并发

- 所有输入 `Value` 在写入前深拷贝；`At`/`Scan`/`Snapshot` 返回的 `Value`
  均为新副本，调用方修改输入或输出都不会影响账本内部状态。
- 所有公开方法通过 `sync.RWMutex` 保护：写操作独占，读操作并发；
  已通过 `go test -race ./...` 验证。

## 复杂度汇总

| 操作 | 复杂度 |
| --- | --- |
| ApplyBatch（m 条变更，n 条存量） | O(n) 克隆 + O(m·n) 插入移动 + O(n) 终态检查 |
| At | O(log n) |
| Scan（返回 k 条） | O(log n + k) |
| Snapshot | O(n) |

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```

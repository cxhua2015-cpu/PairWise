# policycatalog

并发安全的内存型策略目录（Go 1.22+，仅标准库）。原子批次按输入顺序执行
Put/Delete；Put 分配连续 revision，Delete 不分配。完整结构校验先于状态读取，
记录数与 Value 总字节容量只在批次末检查；失败回滚全部状态、generation 与
revision。详见 `SPEC.md`。

## 设计说明

- **索引**：单一路由 `map[string]Record`，按名称 O(1) 定位；`Snapshot` 与
  `Result.Changed` 在读取时按名称排序，不为排序维护额外索引结构。
- **候选事务**：`Apply` 先做整批结构校验（不触碰状态），再在写锁内把当前
  `map` 浅拷贝为候选副本，顺序应用全部 Op 并推进候选 revision；仅在批次末
  对候选副本检查 `MaxRecords` 与 `MaxTotalValueBytes`。任一阶段失败直接丢弃
  候选，已提交的 records、generation、nextRevision 天然不变，无需显式回滚。
- **所有权**：Put 时深拷贝 `Value` 存入；`Get`/`Snapshot`/`Result.Changed`
  返回的 `Value` 与 `Records` 均为新分配副本，调用方对返回值的修改不影响
  内部状态，内部状态也不受调用方后续修改影响。
- **并发**：`sync.RWMutex` 保护全部状态；`Apply` 持写锁，`Get`/`Snapshot`
  持读锁，可多读者并发。空批次不加锁读、不递增 generation。
- **复杂度**：结构校验 O(Σ 名称与值长度)；候选拷贝 O(R)，R 为当前记录数；
  应用与容量检查 O(B + R)，B 为批内 Op 数；`Get` O(1)；`Snapshot` 与
  `Changed` 排序 O(R log R)。空间 O(R + 批次数)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```

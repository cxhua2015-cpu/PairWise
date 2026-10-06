# metacatalog211

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。原子批次按输入顺序执行
Put/Delete，Put 分配连续递增的 revision，Delete 不分配；失败时整体回滚
状态、generation 与 revision。

## 设计

**索引**：`Store` 内部以 `map[string]entry` 作为主索引，键为记录名，值保存
深拷贝后的 Value 与 revision，另维护 `generation`、`nextRev` 与
`totalBytes` 计数器。单把 `sync.Mutex` 保护全部内部状态，所有公开方法
（`Apply`/`Get`/`Snapshot`）均可并发调用。

**候选事务**：`Apply` 分三个阶段——
1. 结构校验：在不读取任何状态的情况下校验全部 Op 的 kind、名称字符集
   （非空 ASCII 小写字母/数字/连字符/下划线，且不超过 `MaxNameBytes`）
   与 Value 长度；任何失败返回 `ErrInvalidInput`。
2. 候选执行：克隆索引后在副本上按输入顺序应用操作，Put 从 `nextRev`
   连续分配 revision，Delete 缺失时返回 `ErrNotFound`。
3. 批次末容量检查：最终记录数与 Value 总字节数超限返回 `ErrCapacity`。

任一阶段失败都不触碰真实状态，天然回滚；全部通过才一次性提交，非空成功
批次 generation 恰好加一，空批次不改变 generation。`Result.Changed` 按
名称排序，包含每个被触及名称的最终记录；被删除的名称以零值 Record
（`Value == nil`）作为墓碑。

**所有权**：所有进入 Store 的 Value 在 Put 时深拷贝，所有离开 Store 的
Value（`Get`、`Snapshot`、`Result.Changed`）同样深拷贝，返回切片与内部
状态完全隔离，调用方修改互不影响。

**复杂度**（n = 记录数，k = 批次内 Op 数）：
- `Apply`：O(n + k) 时间（克隆索引 + 应用操作），O(n) 额外空间。
- `Get`：O(1) 均摊 + O(|value|) 拷贝。
- `Snapshot`：O(n log n)（按名称排序）+ O(总字节数) 拷贝。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```

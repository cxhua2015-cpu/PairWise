# routecatalog

并发安全的内存型路由目录（Go 1.22+，仅标准库）。原子批次按输入顺序执行 Put/Delete，Put 分配连续 revision，Delete 不分配；失败时状态、generation 与 revision 全部回滚。

## 设计说明

- **索引**：`Store` 内部以 `map[string]Record` 作为主索引，按名称 O(1) 定位记录；另维护 `totalBytes` 累计值，避免每次容量检查都全表扫描。Snapshot 与 Result.Changed 在返回前按名称排序。
- **候选事务**：`Apply` 先在持锁状态下把当前 map 浅拷贝为候选副本（Record 的 Value 切片不可变共享，见下），按输入顺序在副本上应用全部 Put/Delete；仅在批次末对最终状态检查记录数与 Value 总字节上限。任何一步失败直接丢弃副本返回错误，已提交状态、generation、revision 零成本回滚。全部成功才一次性替换 map 并单调递增 generation（非空成功批次只增一次）。
- **所有权**：Put 时深拷贝调用方传入的 Value；Get/Snapshot 返回深拷贝的 Value，返回的切片与内部状态完全隔离。候选副本只共享从未暴露过的内部 Value 切片，因此浅拷贝是安全的。
- **校验顺序**：批次先完整做结构校验（kind、名称字符集与长度、Value 长度上限），期间不读取任何状态；结构非法一律 `ErrInvalidInput`，其后才进入状态阶段（`ErrNotFound` / `ErrCapacity`）。
- **并发**：单把 `sync.RWMutex` 保护全部状态；Apply 持写锁，Get/Snapshot 持读锁可并行。

## 复杂度

- `Apply`：时间 O(n + m)，n 为批次数、m 为当前记录数（候选拷贝）；空间 O(m)。
- `Get`：O(1) 均摊（外加返回值拷贝 O(|Value|)）。
- `Snapshot`：O(m log m) 排序 + O(总字节数) 深拷贝。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```

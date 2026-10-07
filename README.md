# metacatalog361

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**：`Store` 以 `map[string]Record` 作为主索引，按名称 O(1) 定位记录；revision 与 generation 为 `Store` 上的单调计数器（`nextRevision` 从 1 开始）。`Snapshot`/`Result.Changed` 在返回前对名称排序，不维护额外的有序结构。

**候选事务**：`Apply` 分两阶段。第一阶段对全部 Op 做完整结构校验（kind、名称字符集与长度、Value 长度），不读取任何状态；失败返回 `ErrInvalidInput`。第二阶段在互斥锁内把当前 map 浅拷贝为候选副本，按输入顺序在副本上执行 Put/Delete：Put 从候选 revision 计数器分配连续 revision，Delete 不分配、目标不存在即 `ErrNotFound`。批次末才检查最终记录数与 Value 总字节容量（`ErrCapacity`）。任何失败直接丢弃候选副本——已提交状态、generation、revision 天然不变；成功才整体换入候选副本，非空批次 generation 恰好加一。

**所有权**：Put 的 Value 在提交前深拷贝，调用方之后修改入参不影响目录；`Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为深拷贝，调用方修改返回值不影响内部状态。返回的切片每次新建，与内部状态完全隔离。

**并发**：所有公开方法通过单一 `sync.Mutex` 串行化状态访问；结构校验在锁外完成。`-race` 下测试通过。

**复杂度**（n = 记录数，k = 批次内 Op 数，B = Value 总字节）：
- `Apply`：校验 O(k·名称长度)；执行 O(n + k)（候选拷贝 + 逐 Op O(1)）；容量检查 O(n)；结果排序 O(k log k)。拷贝 Value 共 O(B)。
- `Get`：O(1) 查找 + O(|Value|) 深拷贝。
- `Snapshot`：O(n log n) 排序 + O(B) 深拷贝。
- 空间：O(n + B)，候选事务期间临时翻倍。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```

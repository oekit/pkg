// Package tarx 目录 tar 传输三件套：同口径预扫 / 流式写出 / 防御式解包，
// 外加可选的 sha256 manifest 自证（#326）。收编自 jumpserver-exec 目录传
// 输层手搓的三件同型逻辑（readDirTar/safeExtractPath/PrescanDir/逐文件摘
// 要清单），任何「把目录从 A 搬到 B」的工具都要重新面对这三个问题：
//
//   - **防御式解包** Extract：绝对路径与 `..` 穿越条目**跳过并计数、点名
//     前三个**（SkippedNames），绝不写 root 之外——Zip Slip 是所有解包不
//     可信归档的工具的共同威胁；非常规条目（symlink/设备/socket/fifo）
//     同样跳过并计数（链接目标逃逸 root 的问题面整类消失）；
//   - **同口径预扫** Scan：本地行走，按「流式写入器会怎么分类」数
//     files/bytes/skipped，给演练答复与流截止时间放大当底数——口径漂移
//     的坑（预扫与写入器各走各的分类，演练就在对它要预演的那次运行撒
//     谎）由「同一棵分类树、同一份实现」钉死：Scan 与 Write 共用 walkRel
//     与同一套分类分支，Scan↔Write 的 Stats 逐字段相等有奇偶测试随包钉住；
//   - **manifest 自证** Write 可在流尾夹带逐文件 sha256 清单（sha256sum
//     文本格式，远端可直接 `sha256sum -c` 人工核对），配 VerifyManifest
//     在另一端程序化核对——「传输完成」与「传输被证明」是两回事。
//
// 用法：
//
//	st, _ := tarx.Scan(root)            // 演练：这次传输有多大
//	st, _ := tarx.Write(w, root, tarx.WithManifest()) // 流式写出 + 自证清单
//	st, _ := tarx.Extract(r, root)      // 防御式解包（SkippedNames 点名恶意条目）
//	err := tarx.VerifyManifest(root)    // 另一端核对（ErrMismatch 指名文件）
//
// # 边界声明
//
//   - **不做压缩**：gzip 等由调用方包一层（w/r 两端各自包，本包只见 tar
//     字节流）；
//   - **不做远端执行**：`sha256sum -c` 之类核对命令是调用方的事（manifest
//     的文本格式就是为了让远端现有的工具能直接消费）；
//   - **Extract 不做 staging/原子交付**：解到哪、何时可见是调用方的文件
//     系统策略（典型做法：先解到 root-adjacent 临时目录再 rename 交付）；
//   - 不背「root 内既有符号链接把写入引到 root 外」的 TOCTOU：Extract
//     的穿越防线针对**归档条目名**；root 目录本身的可信性是调用方的前置
//     条件（fresh 目录/staging 是推荐姿势）。
//
// Stats 语义：三个原语共用一个 Stats（Files/Dirs/Bytes/Skipped/
// SkippedNames）——Scan 数的是「Write 会怎么流式分类」，Extract 数的是
// 「流里的条目怎么落了地」，同型字段逐一对得上；manifest 条目是流协议
// 开销、不算源树内容，不计入 Stats。SkippedNames 只留**前 3 个**点名样本
// （恶意归档动辄上百条穿越，全量名单是调用方日志的事，库只负责给出足够
// 排查的头几个）。
package tarx

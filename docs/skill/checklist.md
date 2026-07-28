# Skill 实现检查清单

> 依据: [Skill 系统设计](README.md)

---

## 加载与模型

- [x] `SKILL.md` strict frontmatter、固定字节上限、name/目录一致和 JSON-compatible options 校验
- [x] 只扫描直接子目录，稳定排序，拒绝 symlink/路径逃逸
- [x] `Status` 只使用 `loaded|disabled` string
- [x] Load 使用临时 map，全部验证成功后一次发布不可变 Manager
- [x] Get/List/Resolve 返回深拷贝且并发只读通过 race test

## 依赖与配置

- [x] Skill 依赖存在、无环、拓扑顺序稳定且共享依赖去重
- [x] Agent Skill allowlist 精确；空数组表示不使用 Skill
- [x] 递归 Skill 依赖也必须在 Agent allowlist
- [x] Tool 依赖存在、enabled 且通过 Agent Tool allowlist
- [x] options 按 frontmatter → root → Agent 顶层 shallow merge
- [x] options 敏感 key、JSON 类型和 64 KiB 合并后上限校验
- [ ] 全部 Skill 文件/配置变化标记 restart-required

## Agent 集成

- [x] Skill messages 顺序、标题、options JSON 和 body 投影确定
- [x] Skill system messages 是 Context protected units
- [x] Prompt/options 不写 Session snapshot，Restore 后可重建
- [x] Skill 不增加 Provider/Tool retry、执行器或隐藏 turn
- [x] 资源访问只经过已有 File/Shell Tool 安全边界

## Remote 与观测

- [x] Remote 只注册两个 GET，并使用固定 `SkillSummary/SkillView`
- [x] status 为稳定 string；path/options/Secret/internal cause 不进入 DTO
- [x] 没有 Skill mutation Tool、runtime Registry、reload watcher或 Skill SSE
- [x] 指标统一 `yaa_skill_*`，无 Skill/Agent/path 高基数 label

## 门禁

- [x] Skill 文档不存在 install/uninstall/enable/disable/reload/invoke route 或 Tool 残留
- [x] JSON/YAML fence 可解析，相对链接和 anchor 存在
- [x] `git diff --check` 通过

---

*最后更新: 2026-07-22*

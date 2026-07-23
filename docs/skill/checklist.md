# Skill 实现检查清单

> 依据: [Skill 系统设计](README.md)

---

## 加载与模型

- [ ] `SKILL.md` strict frontmatter、固定字节上限、name/目录一致和 JSON-compatible options 校验
- [ ] 只扫描直接子目录，稳定排序，拒绝 symlink/路径逃逸
- [ ] `Status` 只使用 `loaded|disabled` string
- [ ] Load 使用临时 map，全部验证成功后一次发布不可变 Manager
- [ ] Get/List/Resolve 返回深拷贝且并发只读通过 race test

## 依赖与配置

- [ ] Skill 依赖存在、无环、拓扑顺序稳定且共享依赖去重
- [ ] Agent Skill allowlist 精确；空数组表示不使用 Skill
- [ ] 递归 Skill 依赖也必须在 Agent allowlist
- [ ] Tool 依赖存在、enabled 且通过 Agent Tool allowlist
- [ ] options 按 frontmatter → root → Agent 顶层 shallow merge
- [ ] options 敏感 key、JSON 类型和 64 KiB 合并后上限校验
- [ ] 全部 Skill 文件/配置变化标记 restart-required

## Agent 集成

- [ ] Skill messages 顺序、标题、options JSON 和 body 投影确定
- [ ] Skill system messages 是 Context protected units
- [ ] Prompt/options 不写 Session snapshot，Restore 后可重建
- [ ] Skill 不增加 Provider/Tool retry、执行器或隐藏 turn
- [ ] 资源访问只经过已有 File/Shell Tool 安全边界

## Remote 与观测

- [ ] Remote 只注册两个 GET，并使用固定 `SkillSummary/SkillView`
- [ ] status 为稳定 string；path/options/Secret/internal cause 不进入 DTO
- [ ] 没有 Skill mutation Tool、runtime Registry、reload watcher或 Skill SSE
- [ ] 指标统一 `yaa_skill_*`，无 Skill/Agent/path 高基数 label

## 门禁

- [ ] Skill 文档不存在 install/uninstall/enable/disable/reload/invoke route 或 Tool 残留
- [ ] JSON/YAML fence 可解析，相对链接和 anchor 存在
- [ ] `git diff --check` 通过

---

*最后更新: 2026-07-22*

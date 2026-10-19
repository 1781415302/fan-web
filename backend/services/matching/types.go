package matching

// Engine 选择匹配引擎；环境变量空/"v2" 为 V2，"legacy" 为 Legacy。
type Engine string

const (
	EngineLegacy Engine = "legacy"
	EngineV2     Engine = "v2"
)

// Source 标明标题/证据来自文件名、目录、catalog 或条目本身。
type Source string

const (
	SourceFile      Source = "file"
	SourceDirectory Source = "directory"
	SourceCatalog   Source = "catalog"
	SourceSubject   Source = "subject"
)

// MediaKind 媒体形态。TV/WEB 同属 KindSeries，平台不同不能互相否定。
type MediaKind string

const (
	KindUnknown MediaKind = "unknown"
	KindSeries  MediaKind = "series"
	KindMovie   MediaKind = "movie"
	KindOVA     MediaKind = "ova"
	KindRecap   MediaKind = "recap"
	KindPromo   MediaKind = "promo"
)

// Ordinal 中的 0 表示未知，不推断为 1。
type Ordinal struct {
	Season, Part int
	Final        bool
}

// Title 保留原语言文本及其来源。
type Title struct {
	Text   string
	Source Source
}

// Numbering 描述集号 token 的形态。
type Numbering string

const (
	NumberUnknown  Numbering = "unknown"
	NumberSingle   Numbering = "single"
	NumberRange    Numbering = "range"
	NumberFraction Numbering = "fraction"
	NumberSpecial  Numbering = "special"
)

// EpisodeToken 文件名中的集号解释。
// Single: First==Last>0；Range: 0<First<Last；Fraction/Special 不强制转整数。
type EpisodeToken struct {
	Kind        Numbering
	First, Last int
	Raw         string
	Explicit    bool // SxxEyy/第N话/方括号集号/明确分隔集号
}

// FileEvidence 单文件解析证据；Conflicts 为中文可解释原因。
type FileEvidence struct {
	Name      string  // 不含目录，原文件名
	Titles    []Title // 保留原语言；目录标题与文件标题来源区分
	Ordinal   Ordinal
	Year      int
	Media     MediaKind
	Episode   EpisodeToken
	Conflicts []string
}

// FileInput 已验证相对目录（OS 分隔符）+ 文件名。
type FileInput struct {
	Name, Dir string
}

// Query 组级查询证据。
type Query struct {
	Titles       []Title
	Ordinal      Ordinal
	Year         int
	Media        MediaKind
	Episodes     []int // 去重升序；只含明确 Single
	HasFileTitle bool
	Conflicts    []string
}

// Group 稳定分组及其组级查询。
type Group struct {
	Key, Dir string
	Files    []FileEvidence
	Query    Query
}

// Subject Bangumi 条目视图；Detailed 仅详情接口构造时为 true。
type Subject struct {
	ID                       int
	Name, NameCn             string
	Aliases                  []string
	Year                     int
	Media                    MediaKind
	EpisodeCount             int // 与 GetSubject 口径一致；未知 0
	Summary, Cover, Platform string
	Detailed                 bool
}

// Chapter 章节条目（运行内核对用）。
type Chapter struct {
	ID       int
	Ep, Sort float64
}

// QueryTerm 有预算的查询词。
type QueryTerm struct {
	Text      string
	Source    Source
	Shortened bool
}

// NameMatchKind 名称命中种类（由评分选择最佳命中时填写）。
type NameMatchKind string

const (
	NameNone       NameMatchKind = "none"
	NameFullStrict NameMatchKind = "full_strict"
	NameFullNormal NameMatchKind = "full_normal"
	NameBaseStrict NameMatchKind = "base_strict"
	NameBaseNormal NameMatchKind = "base_normal"
	NameCatalog    NameMatchKind = "catalog"
	NameSubtitle   NameMatchKind = "subtitle"
	NamePrefix     NameMatchKind = "prefix"
	NameFuzzy      NameMatchKind = "fuzzy"
	NameLoose      NameMatchKind = "loose"
)

// SubjectNameRole 区分条目主名与别名。
type SubjectNameRole string

const (
	SubjectPrimary SubjectNameRole = "primary" // Name 或 NameCn
	SubjectAlias   SubjectNameRole = "alias"
)

// NameEvidence 最佳名称命中的来源与种类；零值表示尚未填写。
// QueryText/SubjectText 保存实际参与最佳命中的输入标题文本，不保存剥离后的 base。
type NameEvidence struct {
	Kind          NameMatchKind
	QueryText     string
	SubjectText   string
	QuerySource   Source
	SubjectRole   SubjectNameRole
	DirectSupport bool // 至少一个原始文件标题/合法目录兜底以 full/base/catalog 强等价支持本候选
}

// MatchRule 已满足条件的特殊推论标签；不表示已通过其他安全门。
type MatchRule string

const (
	RuleFirstSeasonCompat MatchRule = "first_season_compat"
	RuleSubtitleVerified  MatchRule = "subtitle_verified"
	RulePartByChapters    MatchRule = "part_by_chapters"
	RuleVariantPreference MatchRule = "variant_preference"
)

// Candidate 排序后的候选；Blockers 不可忽略，Missing 为缺证据。
// NameEvidence/NeedsChapterProof/AppliedRules 为零值时不得单独导致拒绝。
type Candidate struct {
	Subject           Subject
	NameScore, Score  float64
	Blockers, Missing []string
	Support           []string
	NameEvidence      NameEvidence // 最佳名称命中的来源和种类
	NeedsChapterProof bool         // 新规则尚需真实章节或可信编号绑定才能成立
	AppliedRules      []MatchRule  // 已满足条件的特殊推论；不表示已通过其他安全门
}

// DecisionCode 统一决策结果码。
type DecisionCode string

const (
	Accepted     DecisionCode = "accepted"
	Review       DecisionCode = "review"
	NoCandidates DecisionCode = "no_candidates"
)

// Decision 自动/显式确认的统一输出；Candidates 无候选时须为非 nil 空切片。
type Decision struct {
	Code       DecisionCode
	WinnerID   int // 只有 Accepted 时 >0
	Candidates []Candidate
	Reason     string
}

// EpisodeBinding 文件→集号绑定；StoredNumber 保留文件 Single 原始正整数。
type EpisodeBinding struct {
	FileName     string
	StoredNumber int
	ChapterID    int    // 仅本次匹配核对/去重，不写入现有 schema
	Basis        string // "ep" / "sort" / "count" / "movie"
}

// ChapterState 章节核对状态。
type ChapterState string

const (
	ChapterUnchecked ChapterState = "unchecked"
	ChapterCountFit  ChapterState = "count_fit"
	ChapterConfirmed ChapterState = "confirmed"
	ChapterConflict  ChapterState = "conflict"
)

// ChapterCoverage 组内集号覆盖；未知章节时 Required 可填、Matched 空。
// count/movie 占位不能填入 Matched。
type ChapterCoverage struct {
	Required []int // 当前组所有 NumberSingle 正整数编号，去重升序，含越界或重复版本对应号
	Matched  []int // 有唯一且未被拒绝的真实 ep/sort + 正 ChapterID 绑定的编号，去重升序
	Missing  []int // Required - Matched
}

// ChapterDecision 目录级章节核对结果。
type ChapterDecision struct {
	State    ChapterState
	Bindings []EpisodeBinding
	Rejected map[string]string // 文件名→原因
	Coverage ChapterCoverage   // 未知章节时 Required 可填、Matched 空
}

// CandidatePolicy 纯策略评估结果（由 policy 填充；零值不改变既有决策）。
type CandidatePolicy struct {
	Blockers, Missing, Support []string
	AppliedRules               []MatchRule // 本阶段已证明可用的规则许可
	NeedsChapterProof          bool
}

// BindingBatch 一批绑定（全库扫描只能保护，不能删除/改号）。
type BindingBatch struct {
	Dir       string
	SubjectID int
	Existing  bool
	Bindings  []EpisodeBinding
}

// BindingConflict 跨组重复章节冲突。
type BindingConflict struct {
	Dir       string
	SubjectID int
	Files     []string
	Reason    string
}

// AliasFamily 系列别名族（catalog JSON schema）。
type AliasFamily struct {
	Key       string   `json:"key"`
	Canonical string   `json:"canonical"`
	Aliases   []string `json:"aliases"`
}

// Catalog 不透明只读索引；实现字段不构成跨模块契约（S2 填充）。
type Catalog struct {
	families []AliasFamily
	index    map[string]int // Strict/Normal 键 → families 下标
}

// TextForms 严格/常规/宽松比较形态。
type TextForms struct {
	Strict, Normal, Loose string
}

// TitleFacts 从标题抽取的结构事实。
type TitleFacts struct {
	Base           string
	Ordinal        Ordinal
	Year           int
	Media          MediaKind
	Discriminators []string // 不能被忽略的数字/字母/标点身份特征
	Conflicts      []string
}

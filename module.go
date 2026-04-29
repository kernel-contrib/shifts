package shifts

import (
	"io/fs"
	"time"

	"github.com/kernel-contrib/shifts/internal"
	"github.com/kernel-contrib/shifts/migrations"
	"go.edgescale.dev/kernel/sdk"
)

// Module is the main entry point for the shifts kernel module.
// It acts as a declarative scheduler — defines when/where people work,
// decoupled from physical attendance recording.
type Module struct {
	ctx            sdk.Context
	repo           *internal.Repository
	svc            *internal.Service
	reminderTicker *time.Ticker
}

// New constructs the module.
func New() *Module {
	return &Module{}
}

// Manifest returns immutable metadata for this module.
func (m *Module) Manifest() sdk.Manifest {
	return sdk.Manifest{
		ID:          "shifts",
		Type:        sdk.TypeFeature,
		Schema:      "module_shifts",
		Name:        "Shifts",
		Description: "Declarative shift scheduling engine — defines when and where employees work",
		Version:     "1.0.0",
		DependsOn:   []string{"iam"},

		Permissions: []sdk.Permission{
			{Key: "shifts.shifts.read", Label: sdk.T("View shifts", "ar", "عرض الورديات")},
			{Key: "shifts.shifts.manage", Label: sdk.T("Create, update, and delete shifts", "ar", "إنشاء وتعديل وحذف الورديات")},
			{Key: "shifts.members.read", Label: sdk.T("View shift assignments", "ar", "عرض تعيينات الورديات")},
			{Key: "shifts.members.manage", Label: sdk.T("Assign and remove shift members", "ar", "تعيين وإزالة أعضاء الورديات")},
			{Key: "shifts.overrides.read", Label: sdk.T("View shift overrides", "ar", "عرض استثناءات الورديات")},
			{Key: "shifts.overrides.manage", Label: sdk.T("Create, update, and delete overrides", "ar", "إنشاء وتعديل وحذف الاستثناءات")},
			{Key: "shifts.roster.read", Label: sdk.T("View team roster", "ar", "عرض جدول الفريق")},
		},

		PublicEvents: []sdk.EventDef{
			{Subject: "shifts.shift.created", Description: sdk.T("A new shift was created")},
			{Subject: "shifts.shift.updated", Description: sdk.T("A shift was updated")},
			{Subject: "shifts.shift.deleted", Description: sdk.T("A shift was deleted")},
			{Subject: "shifts.member.assigned", Description: sdk.T("A member was assigned to a shift")},
			{Subject: "shifts.member.removed", Description: sdk.T("A member was removed from a shift")},
			{Subject: "shifts.override.created", Description: sdk.T("A shift override was created")},
			{Subject: "shifts.override.updated", Description: sdk.T("A shift override was updated")},
			{Subject: "shifts.override.deleted", Description: sdk.T("A shift override was deleted")},
			{Subject: "shifts.reminder.dispatch", Description: sdk.T("A shift reminder should be sent")},
		},

		Config: []sdk.ConfigFieldDef{
			{
				Key:     "shifts.allow_overlapping_assignments",
				Type:    "bool",
				Default: "false",
				Label:   sdk.T("Allow overlapping assignments", "ar", "السماح بالتعيينات المتداخلة"),
				Description: sdk.T(
					"Allow a member to be assigned to multiple shifts with overlapping days within the same business. Cross-business conflicts are always blocked.",
					"ar", "السماح بتعيين عضو في ورديات متعددة ذات أيام متداخلة ضمن نفس العمل. التعارضات عبر الأعمال محظورة دائماً.",
				),
			},
			{
				Key:     "shifts.reminder_minutes",
				Type:    "text",
				Default: "[30,15,1]",
				Label:   sdk.T("Reminder intervals (minutes)", "ar", "فترات التذكير (دقائق)"),
				Description: sdk.T(
					"JSON array of minutes before shift start to send reminders. The 30-minute reminder always fires regardless of this setting.",
					"ar", "مصفوفة JSON بالدقائق قبل بدء الوردية لإرسال التذكيرات. تذكير الـ 30 دقيقة يعمل دائماً بغض النظر عن هذا الإعداد.",
				),
			},
			{
				Key:     "shifts.early_checkin_allowance_mins",
				Type:    "number",
				Default: "5",
				Label:   sdk.T("Early check-in allowance (minutes)", "ar", "السماح بالحضور المبكر (دقائق)"),
				Description: sdk.T(
					"Tenant-wide default: minutes before shift start a check-in is accepted. Can be overridden per shift.",
					"ar", "الافتراضي على مستوى المنشأة: الدقائق قبل بدء الوردية لقبول تسجيل الحضور. يمكن تجاوزه لكل وردية.",
				),
			},
			{
				Key:     "shifts.late_checkin_grace_mins",
				Type:    "number",
				Default: "5",
				Label:   sdk.T("Late check-in grace (minutes)", "ar", "فترة سماح التأخر بالحضور (دقائق)"),
				Description: sdk.T(
					"Tenant-wide default: minutes after shift start before marking as late. Can be overridden per shift.",
					"ar", "الافتراضي على مستوى المنشأة: الدقائق بعد بدء الوردية قبل اعتباره متأخراً. يمكن تجاوزه لكل وردية.",
				),
			},
			{
				Key:     "shifts.early_checkout_allowance_mins",
				Type:    "number",
				Default: "5",
				Label:   sdk.T("Early check-out allowance (minutes)", "ar", "السماح بالانصراف المبكر (دقائق)"),
				Description: sdk.T(
					"Tenant-wide default: minutes before shift end a check-out is accepted. Can be overridden per shift.",
					"ar", "الافتراضي على مستوى المنشأة: الدقائق قبل نهاية الوردية لقبول تسجيل الانصراف. يمكن تجاوزه لكل وردية.",
				),
			},
			{
				Key:     "shifts.late_checkout_allowance_mins",
				Type:    "number",
				Default: "5",
				Label:   sdk.T("Late check-out allowance (minutes)", "ar", "السماح بالانصراف المتأخر (دقائق)"),
				Description: sdk.T(
					"Tenant-wide default: minutes after shift end a check-out is accepted. Can be overridden per shift.",
					"ar", "الافتراضي على مستوى المنشأة: الدقائق بعد نهاية الوردية لقبول تسجيل الانصراف. يمكن تجاوزه لكل وردية.",
				),
			},
		},

		UINav: []sdk.NavItem{
			{Label: sdk.T("Shifts", "ar", "الورديات"), Icon: "calendar_month", Path: "/shifts", Permission: "shifts.shifts.read", SortOrder: 1},
		},
	}
}

// Migrations returns the embedded SQL migration files.
func (m *Module) Migrations() fs.FS {
	return migrations.FS
}

// Init wires the module's internal services.
func (m *Module) Init(ctx sdk.Context) error {
	m.ctx = ctx
	m.repo = internal.NewRepository(ctx.DB)
	m.svc = internal.NewService(m.repo, ctx.Bus, ctx.Redis, ctx.Config, ctx.Logger)

	// Register cross-module reader.
	ctx.RegisterReader(&shiftsReader{
		svc: m.svc,
	})

	// Start the smart reminders cron.
	m.startReminderCron()

	ctx.Logger.Info("shifts module initialized")
	return nil
}

// Shutdown performs cleanup before the kernel stops.
func (m *Module) Shutdown() error {
	m.stopReminderCron()
	return nil
}

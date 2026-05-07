-- - Shifts (the base schedule definition) ------------------
CREATE TABLE shifts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    title TEXT NOT NULL,
    -- Schedule type:
    --   'permanent'      → repeats weekly on working_days, no end date required
    --   'specific_dates' → uses either a date range or an explicit date list
    shift_type TEXT NOT NULL DEFAULT 'permanent' CHECK (shift_type IN ('permanent', 'specific_dates')),
    -- For specific_dates shifts with a range: defines the active window.
    -- For permanent shifts: start_date = first effective day, end_date = NULL.
    start_date DATE,
    end_date DATE,
    -- Days of week (ISO: 1=Mon, 7=Sun). Stored as smallint array.
    working_days JSONB NOT NULL DEFAULT '[]',
    -- Explicit date list for non-contiguous specific_dates shifts.
    -- When populated, the shift is active ONLY on these exact dates.
    -- Overrides start_date/end_date and working_days.
    -- Example: ['2026-05-05', '2026-05-08', '2026-06-03']
    specific_dates JSONB NOT NULL DEFAULT '[]',
    -- Clock times (NOT timestamps - date is resolved at query time).
    -- If end_time < start_time → overnight shift.
    start_time TIME NOT NULL,
    end_time TIME NOT NULL,
    -- Where the employee works for this shift.
    work_location_type TEXT NOT NULL DEFAULT 'onsite' CHECK (
        work_location_type IN ('onsite', 'remote', 'hybrid', 'field')
    ),
    -- Extensible rules (per-shift override of tenant defaults). Known keys:
    --   early_checkin_allowance_mins  (int)   - minutes before start_time a check-in is accepted
    --   late_checkin_grace_mins       (int)   - minutes after start_time before marking late
    --   early_checkout_allowance_mins (int)   - minutes before end_time a check-out is accepted
    --   late_checkout_allowance_mins  (int)   - minutes after end_time a check-out is accepted
    --   remote_days                   (int[]) - for hybrid, which days are remote
    --
    -- Cascade: shift.metadata -> tenant config -> hard default (5 min)
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);
CREATE INDEX idx_shifts_tenant_id ON shifts(tenant_id)
WHERE deleted_at IS NULL;
-- - Shift Members (user <-> shift assignment with history) -----------
CREATE TABLE shift_members (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    shift_id UUID NOT NULL REFERENCES shifts(id) ON DELETE CASCADE,
    tenant_member_id UUID NOT NULL,
    -- references IAM member (application-level, not FK)
    user_id UUID NOT NULL,
    -- global identity (resolved from IAM at assignment time)
    -- enables cross-tenant conflict detection
    -- Assignment window. Enables historical tracking.
    effective_from DATE NOT NULL DEFAULT CURRENT_DATE,
    effective_to DATE,
    -- NULL = indefinite / current
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);
CREATE INDEX idx_shift_members_tenant_id ON shift_members(tenant_id)
WHERE deleted_at IS NULL;
CREATE INDEX idx_shift_members_shift_id ON shift_members(shift_id)
WHERE deleted_at IS NULL;
CREATE INDEX idx_shift_members_member ON shift_members(tenant_member_id, effective_from, effective_to)
WHERE deleted_at IS NULL;
CREATE INDEX idx_shift_members_user_id ON shift_members(user_id)
WHERE deleted_at IS NULL;
-- - Shift Overrides (date-range exceptions) -----------------
CREATE TABLE shift_overrides (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    shift_id UUID NOT NULL REFERENCES shifts(id) ON DELETE CASCADE,
    -- NULL = applies to ALL members of the shift (e.g., national holiday).
    -- Set = applies to one specific member only.
    tenant_member_id UUID,
    -- Date range this override covers. Contiguous blocks, not single days.
    start_date DATE NOT NULL,
    end_date DATE NOT NULL,
    -- What changes:
    is_day_off BOOLEAN NOT NULL DEFAULT FALSE,
    new_start_time TIME,
    new_end_time TIME,
    reason TEXT,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);
CREATE INDEX idx_shift_overrides_tenant_id ON shift_overrides(tenant_id)
WHERE deleted_at IS NULL;
CREATE INDEX idx_shift_overrides_shift_id ON shift_overrides(shift_id)
WHERE deleted_at IS NULL;
CREATE INDEX idx_shift_overrides_date_range ON shift_overrides(shift_id, start_date, end_date)
WHERE deleted_at IS NULL;

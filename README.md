# Shifts Module

Declarative shift scheduling engine for the [EdgeScale Kernel](https://go.edgescale.dev/kernel). Defines **when and where** employees work - decoupled from physical attendance recording.

## Features

- **Shift Types**: Permanent (recurring weekly) and specific-dates (date range or explicit date list)
- **Cross-Tenant Conflict Detection**: A user assigned across multiple businesses cannot have overlapping shifts
- **Override System**: Date-range exceptions per shift or per member (member-specific > shift-wide > base)
- **Bulk Overrides**: Apply overrides to multiple members × multiple dates in one call
- **Roster Resolution**: Calendar view with 42-day cap (6 weeks), resolves overrides and grace windows
- **Grace Window Cascade**: Shift metadata → tenant config → 5-minute hard default
- **Specific Dates**: Supports both contiguous date ranges and arbitrary non-contiguous date lists
- **Smart Reminders**: Cron-driven reminder events before shift starts (configurable intervals)
- **My Schedule**: Employee self-view of their resolved shifts

## Architecture

```bash
handlers.go        → thin HTTP controllers (bind, validate, delegate)
internal/service.go → business logic, validation, events, conflict detection
internal/repository.go → GORM data access layer
internal/helpers.go → date grouping, override resolution, grace window cascade
types/types.go     → shared domain models (Shift, ShiftMember, ShiftOverride, ResolvedShift)
reader.go          → cross-module read-only API for attendance module
hooks.go           → tenant provisioning hook
cron.go            → smart shift reminders
routes.go          → route registration
module.go          → lifecycle (Manifest, Init, Shutdown)
```

## Schema

Three tables with soft deletes:

| Table | Purpose |
| --- | --- |
| `shifts` | Base schedule definition (title, type, days, times, metadata) |
| `shift_members` | User <-> shift assignment with effective date range and global `user_id` for cross-tenant conflict detection |
| `shift_overrides` | Date-range exceptions; `tenant_member_id = NULL` applies to all members |

## Configuration

Tenant-level config keys (set via kernel config API):

| Key | Type | Default | Description |
| --- | --- | --- | --- |
| `shifts.allow_overlapping_assignments` | bool | `false` | Allow same-tenant overlapping shift assignments |
| `shifts.reminder_minutes` | text | `[30,15,1]` | Minutes before shift start to send reminders |
| `shifts.early_checkin_allowance_mins` | number | `5` | Minutes before shift start a check-in is accepted |
| `shifts.late_checkin_grace_mins` | number | `5` | Minutes after shift start before marking late |
| `shifts.early_checkout_allowance_mins` | number | `5` | Minutes before shift end a check-out is accepted |
| `shifts.late_checkout_allowance_mins` | number | `5` | Minutes after shift end a check-out is accepted |

**Grace Window Cascade**: Shift `metadata` overrides → tenant config → hard default (5 min).

## Events

| Subject | Description |
| --- | --- |
| `shifts.shift.created` | A new shift was created |
| `shifts.shift.updated` | A shift was updated |
| `shifts.shift.deleted` | A shift was deleted |
| `shifts.member.assigned` | A member was assigned to a shift |
| `shifts.member.removed` | A member was removed from a shift |
| `shifts.override.created` | A shift override was created |
| `shifts.override.updated` | A shift override was updated |
| `shifts.override.deleted` | A shift override was deleted |
| `shifts.reminder.dispatch` | A shift reminder should be sent |

## Permissions

| Key | Description |
| --- | --- |
| `shifts.shifts.read` | View shifts |
| `shifts.shifts.manage` | Create, update, and delete shifts |
| `shifts.members.read` | View shift assignments |
| `shifts.members.manage` | Assign and remove shift members |
| `shifts.overrides.read` | View shift overrides |
| `shifts.overrides.manage` | Create, update, and delete overrides |
| `shifts.roster.read` | View team roster |

---

## API Reference

All tenant-scoped routes are prefixed with `/v1/:tenant_id/shifts`.

### Shifts

#### List Shifts

```bash
GET /v1/:tenant_id/shifts/shifts?page=1&page_size=20

# Example
curl -X GET "https://api.example.com/v1/550e8400-e29b-41d4-a716-446655440000/shifts/shifts?page=1&page_size=20" \
  -H "Authorization: Bearer <token>"
```

**Response** `200 OK`:

```json
{
  "data": [
    {
      "id": "a1b2c3d4-0000-0000-0000-000000000001",
      "tenant_id": "550e8400-e29b-41d4-a716-446655440000",
      "title": "Morning Shift",
      "shift_type": "permanent",
      "start_date": null,
      "end_date": null,
      "working_days": [1, 2, 3, 4, 5],
      "specific_dates": [],
      "start_time": "08:00",
      "end_time": "16:00",
      "work_location_type": "onsite",
      "metadata": {}
    }
  ],
  "meta": { "page": 1, "page_size": 20, "total": 1 }
}
```

#### Create Shift (Permanent)

```bash
POST /v1/:tenant_id/shifts/shifts

# Example: Mon-Fri 08:00-16:00 recurring shift
curl -X POST "https://api.example.com/v1/550e8400-e29b-41d4-a716-446655440000/shifts/shifts" \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Morning Shift",
    "shift_type": "permanent",
    "working_days": [1, 2, 3, 4, 5],
    "start_time": "08:00",
    "end_time": "16:00",
    "work_location_type": "onsite",
    "metadata": {
      "early_checkin_allowance_mins": 15,
      "late_checkin_grace_mins": 10
    }
  }'
```

#### Create Shift (Specific Dates - Range)

```bash
# Active only between start_date and end_date on specified working_days
curl -X POST "https://api.example.com/v1/550e8400-e29b-41d4-a716-446655440000/shifts/shifts" \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Ramadan Shift",
    "shift_type": "specific_dates",
    "start_date": "2026-03-01",
    "end_date": "2026-03-30",
    "working_days": [1, 2, 3, 4, 5],
    "start_time": "09:00",
    "end_time": "15:00",
    "work_location_type": "onsite"
  }'
```

#### Create Shift (Specific Dates - Explicit List)

```bash
# Active ONLY on these exact dates (non-contiguous)
curl -X POST "https://api.example.com/v1/550e8400-e29b-41d4-a716-446655440000/shifts/shifts" \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Event Coverage",
    "shift_type": "specific_dates",
    "specific_dates": ["2026-05-05", "2026-05-08", "2026-06-03"],
    "start_time": "10:00",
    "end_time": "18:00",
    "work_location_type": "field"
  }'
```

#### Get Shift

```bash
GET /v1/:tenant_id/shifts/shifts/:id

curl -X GET "https://api.example.com/v1/550e8400-e29b-41d4-a716-446655440000/shifts/shifts/a1b2c3d4-0000-0000-0000-000000000001" \
  -H "Authorization: Bearer <token>"
```

#### Update Shift

```bash
PUT /v1/:tenant_id/shifts/shifts/:id

# Partial update - only provided fields are changed
curl -X PUT "https://api.example.com/v1/550e8400-e29b-41d4-a716-446655440000/shifts/shifts/a1b2c3d4-0000-0000-0000-000000000001" \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Morning Shift (Updated)",
    "start_time": "07:30",
    "end_time": "15:30",
    "specific_dates": ["2026-05-10", "2026-05-12"]
  }'
```

#### Delete Shift

```bash
DELETE /v1/:tenant_id/shifts/shifts/:id

curl -X DELETE "https://api.example.com/v1/550e8400-e29b-41d4-a716-446655440000/shifts/shifts/a1b2c3d4-0000-0000-0000-000000000001" \
  -H "Authorization: Bearer <token>"
```

**Response** `204 No Content`

---

### Members

#### List Members

```bash
GET /v1/:tenant_id/shifts/shifts/:id/members

curl -X GET "https://api.example.com/v1/550e8400-e29b-41d4-a716-446655440000/shifts/shifts/a1b2c3d4-0000-0000-0000-000000000001/members" \
  -H "Authorization: Bearer <token>"
```

**Response** `200 OK`:

```json
{
  "data": [
    {
      "id": "m1m2m3m4-0000-0000-0000-000000000001",
      "tenant_id": "550e8400-e29b-41d4-a716-446655440000",
      "shift_id": "a1b2c3d4-0000-0000-0000-000000000001",
      "tenant_member_id": "u1u2u3u4-0000-0000-0000-000000000001",
      "user_id": "g1g2g3g4-0000-0000-0000-000000000001",
      "effective_from": "2026-01-01",
      "effective_to": null
    }
  ]
}
```

#### Assign Members

```bash
POST /v1/:tenant_id/shifts/shifts/:id/members

# Assign multiple members at once
curl -X POST "https://api.example.com/v1/550e8400-e29b-41d4-a716-446655440000/shifts/shifts/a1b2c3d4-0000-0000-0000-000000000001/members" \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "member_ids": [
      "u1u2u3u4-0000-0000-0000-000000000001",
      "u1u2u3u4-0000-0000-0000-000000000002"
    ],
    "effective_from": "2026-05-01",
    "effective_to": "2026-12-31"
  }'
```

**Response** `201 Created`

**Error** `409 Conflict` (cross-tenant conflict):

```json
{
  "error": {
    "code": 409,
    "message": "this user has shift conflicts with a shift in another business"
  }
}
```

#### Remove Member

```bash
DELETE /v1/:tenant_id/shifts/shifts/:id/members/:member_id

curl -X DELETE "https://api.example.com/v1/550e8400-e29b-41d4-a716-446655440000/shifts/shifts/a1b2c3d4-0000-0000-0000-000000000001/members/m1m2m3m4-0000-0000-0000-000000000001" \
  -H "Authorization: Bearer <token>"
```

**Response** `204 No Content`

---

### Overrides

#### List Overrides

```bash
GET /v1/:tenant_id/shifts/shifts/:id/overrides

curl -X GET "https://api.example.com/v1/550e8400-e29b-41d4-a716-446655440000/shifts/shifts/a1b2c3d4-0000-0000-0000-000000000001/overrides" \
  -H "Authorization: Bearer <token>"
```

#### Create Override (Whole Shift - National Holiday)

```bash
POST /v1/:tenant_id/shifts/shifts/:id/overrides

# All members get the day off
curl -X POST "https://api.example.com/v1/550e8400-e29b-41d4-a716-446655440000/shifts/shifts/a1b2c3d4-0000-0000-0000-000000000001/overrides" \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "dates": ["2026-05-01"],
    "is_day_off": true,
    "reason": "Labour Day"
  }'
```

#### Create Override (Single Member - Schedule Change)

```bash
# Ali has a doctor appointment, shifts to 10:00-18:00
curl -X POST "https://api.example.com/v1/550e8400-e29b-41d4-a716-446655440000/shifts/shifts/a1b2c3d4-0000-0000-0000-000000000001/overrides" \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "dates": ["2026-05-05"],
    "tenant_member_ids": ["u1u2u3u4-0000-0000-0000-000000000001"],
    "is_day_off": false,
    "new_start_time": "10:00",
    "new_end_time": "18:00",
    "reason": "Doctor appointment - late start"
  }'
```

#### Create Override (Bulk Members - Training Day)

```bash
# 3 members get the day off on 2 dates
curl -X POST "https://api.example.com/v1/550e8400-e29b-41d4-a716-446655440000/shifts/shifts/a1b2c3d4-0000-0000-0000-000000000001/overrides" \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "dates": ["2026-05-05", "2026-05-06"],
    "tenant_member_ids": [
      "u1u2u3u4-0000-0000-0000-000000000001",
      "u1u2u3u4-0000-0000-0000-000000000002",
      "u1u2u3u4-0000-0000-0000-000000000003"
    ],
    "is_day_off": true,
    "reason": "Training day"
  }'
```

**Response** `201 Created` - Returns 3 override records (1 per member × 1 contiguous date range).

#### Update Override

```bash
PUT /v1/:tenant_id/shifts/overrides/:id

curl -X PUT "https://api.example.com/v1/550e8400-e29b-41d4-a716-446655440000/shifts/overrides/o1o2o3o4-0000-0000-0000-000000000001" \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "is_day_off": false,
    "new_start_time": "09:00",
    "new_end_time": "17:00",
    "reason": "Changed to half-day"
  }'
```

#### Delete Override

```bash
DELETE /v1/:tenant_id/shifts/overrides/:id

curl -X DELETE "https://api.example.com/v1/550e8400-e29b-41d4-a716-446655440000/shifts/overrides/o1o2o3o4-0000-0000-0000-000000000001" \
  -H "Authorization: Bearer <token>"
```

**Response** `204 No Content`

---

### Roster

#### Get Roster (Manager View)

Returns resolved shifts for all members in the tenant, applying override resolution.

```bash
GET /v1/:tenant_id/shifts/roster?start_date=2026-05-01&end_date=2026-05-14&shift_id=<optional>

curl -X GET "https://api.example.com/v1/550e8400-e29b-41d4-a716-446655440000/shifts/roster?start_date=2026-05-01&end_date=2026-05-14" \
  -H "Authorization: Bearer <token>"
```

**Response** `200 OK`:

```json
{
  "data": [
    {
      "shift_id": "a1b2c3d4-0000-0000-0000-000000000001",
      "shift_title": "Morning Shift",
      "tenant_id": "550e8400-e29b-41d4-a716-446655440000",
      "tenant_member_id": "u1u2u3u4-0000-0000-0000-000000000001",
      "user_id": "g1g2g3g4-0000-0000-0000-000000000001",
      "date": "2026-05-01",
      "start_time": "08:00",
      "end_time": "16:00",
      "is_day_off": false,
      "is_overnight": false,
      "work_location_type": "onsite",
      "override_id": null,
      "override_reason": null,
      "grace_window": {
        "early_checkin_allowance_mins": 15,
        "late_checkin_grace_mins": 10,
        "early_checkout_allowance_mins": 5,
        "late_checkout_allowance_mins": 5
      }
    }
  ]
}
```

> **Note**: Max date range is 42 days (6 weeks) for performance.

---

### My Schedule

#### Get My Schedule (Employee Self-View)

```bash
GET /v1/:tenant_id/shifts/my-schedule?start_date=2026-05-01&end_date=2026-05-14

curl -X GET "https://api.example.com/v1/550e8400-e29b-41d4-a716-446655440000/shifts/my-schedule?start_date=2026-05-01&end_date=2026-05-14" \
  -H "Authorization: Bearer <token>"
```

**Response** `200 OK`: Same format as roster, but filtered to the authenticated user's shifts.

---

## Cross-Module Reader

Other modules (e.g., attendance) consume shifts data via the reader interface:

```go
reader, err := sdk.Reader[shifts.ShiftsReader](&m.ctx, "shifts")

// Get resolved shift for a member on a specific day
resolved, err := reader.GetShiftForDay(ctx, tenantID, memberID, date)

// Get all shifts starting within the next hour (used by reminders)
upcoming, err := reader.GetShiftsStartingWithinHour(ctx, time.Now())
```

The `ResolvedShift` includes pre-resolved `GraceWindow` values - the attendance module doesn't need to know about the cascade.

## Testing

```bash
go test -v ./...
```

22 tests covering: CRUD, cross-tenant conflict detection, override conflict detection, bulk member overrides, roster resolution with overrides, grace window cascade, specific dates (contiguous and non-contiguous), tenant isolation.

## Requirements

- Go 1.26+
- EdgeScale Kernel SDK
- Depends on: `iam` module (for user identity resolution)

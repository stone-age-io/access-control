# Idempotent demo seed for the stone-access dev instance.
#
# Adds a believable multi-site company on top of the base fixture's HQ data: two more
# sites, their controllers and portals, an armed area with intrusion points, a role/access
# group graph, thirteen people with credentials, badge logins, visitor passes, and a
# backfill of recent events plus unacknowledged alarms.
#
# Safe to re-run: every record is looked up by its natural key and only created if
# missing, so a partial run can simply be repeated.
#
# Requires the `pb` CLI authenticated as a superuser against the target instance.
# Pair it with demo/access-demo.yaml (rule-router) to keep the event feed live; see
# demo/README.md.
#
# --- SCHEMA NOTE: cardholders is an AUTH collection -------------------------------
# One person is one record whether or not they can sign in. Two consequences here:
#
#   * Every cardholder create must carry `password` + `passwordConfirm`. PocketBase's
#     record-create request form makes both required on any new auth record, and that
#     validation runs before accessd's own hooks, so the caller has to supply them.
#     New-Cardholder below fills a random one; nobody ever sees it.
#   * Being an auth record is not being an account. The collection's auth rule requires
#     `badge_login`, so the people below who do not have it cannot sign in at all,
#     whatever password is stored.
# ----------------------------------------------------------------------------------

$script:created = 0; $script:skipped = 0; $script:failed = 0
$ID = @{}  # code/key -> record id

function Get-Id {
  param([string]$Coll, [string]$Field, [string]$Value)
  $flt = '{0}="{1}"' -f $Field, $Value
  try {
    $r = pb collections list $Coll --filter $flt --output json 2>$null | ConvertFrom-Json
    if ($r.totalItems -ge 1) { return $r.items[0].id }
  } catch {}
  return $null
}

function New-Rec {
  param([string]$Coll, [hashtable]$Data, [string]$Field = 'code', [string]$Value)
  if (-not $Value) { $Value = [string]$Data[$Field] }
  $existing = Get-Id $Coll $Field $Value
  if ($existing) {
    $script:skipped++; Write-Host ("  = {0,-16} {1,-22} {2}" -f $Coll, $Value, $existing)
    return $existing
  }
  $json = $Data | ConvertTo-Json -Depth 12 -Compress
  try {
    $res = $json | pb collections create $Coll --output json 2>$null | ConvertFrom-Json
  } catch { $res = $null }
  if ($res -and $res.id) {
    $script:created++; Write-Host ("  + {0,-16} {1,-22} {2}" -f $Coll, $Value, $res.id)
    return $res.id
  }
  # Re-run showing the error for diagnostics.
  $err = $json | pb collections create $Coll --output json 2>&1 | Out-String
  $script:failed++; Write-Host ("  ! {0,-16} {1} FAILED: {2}" -f $Coll, $Value, $err.Trim())
  return $null
}

# New-Cardholder wraps New-Rec for the one collection that needs a password on create.
# Random rather than a constant: a shared default across an install's records would be a
# de-facto shared secret the moment badge_login was ticked for anyone.
function New-Cardholder {
  param([hashtable]$Data)
  if (-not $Data.ContainsKey('password')) {
    $bytes = New-Object 'byte[]' 24
    [System.Security.Cryptography.RandomNumberGenerator]::Fill($bytes)
    $pw = [Convert]::ToBase64String($bytes)
    $Data['password'] = $pw
    $Data['passwordConfirm'] = $pw
  } else {
    $Data['passwordConfirm'] = $Data['password']
  }
  return New-Rec cardholders $Data email ([string]$Data['email'])
}

# Set-Rec patches an existing record (used for the additive touch-ups below).
function Set-Rec {
  param([string]$Coll, [string]$Id, [hashtable]$Data, [string]$What)
  if (-not $Id) { return }
  $json = $Data | ConvertTo-Json -Depth 8 -Compress
  $json | pb collections update $Coll $Id --output json 2>$null | Out-Null
  Write-Host ("  ~ {0,-16} {1}" -f $Coll, $What)
}

Write-Host "`n== Resolve existing records =="
$ID['hq']            = Get-Id locations code hq
$ID['ctrl-hq-1']     = Get-Id controllers code ctrl-hq-1
$ID['business-hours']= Get-Id schedules code business-hours
$ID['staff']         = Get-Id roles code staff
$ID['lobby-group']   = Get-Id access_groups code lobby-group
$ID['lobby-main']    = Get-Id portals code lobby-main
$ID['lobby-public']  = Get-Id portals code lobby-public
$ID['alice']         = Get-Id cardholders email alice@example.com
foreach ($k in 'hq','ctrl-hq-1','staff','lobby-main') {
  if (-not $ID[$k]) { Write-Host "  ! missing expected existing record: $k"; }
  else { Write-Host ("  . {0,-16} {1}" -f $k, $ID[$k]) }
}

Write-Host "`n== Schedules =="
$ID['office-hours']   = New-Rec schedules @{ code='office-hours';   name='Office Hours (Mon-Fri 07:00-19:00)';  ignore_holidays=$false; windows=@(@{days=@(1,2,3,4,5);     start='07:00'; end='19:00'}) }
$ID['extended-hours'] = New-Rec schedules @{ code='extended-hours'; name='Extended Hours (Mon-Sat 06:00-22:00)';ignore_holidays=$false; windows=@(@{days=@(1,2,3,4,5,6);   start='06:00'; end='22:00'}) }
$ID['cleaning-crew']  = New-Rec schedules @{ code='cleaning-crew';  name='Cleaning Crew (Tue & Thu 20:00-23:00)';ignore_holidays=$false;windows=@(@{days=@(2,4);           start='20:00'; end='23:00'}) }
$ID['always']         = New-Rec schedules @{ code='always';         name='24/7 Access';                         ignore_holidays=$true;  windows=@(@{days=@(1,2,3,4,5,6,7); start='00:00'; end='00:00'}) }
$ID['weekends']       = New-Rec schedules @{ code='weekends';       name='Weekends (Sat-Sun 09:00-17:00)';       ignore_holidays=$false; windows=@(@{days=@(6,7);          start='09:00'; end='17:00'}) }

Write-Host "`n== Holiday calendar + holidays =="
$ID['us-federal'] = New-Rec holiday_calendars @{ code='us-federal'; name='US Federal Holidays' }
if ($ID['us-federal']) {
  $cal = $ID['us-federal']
  New-Rec holidays @{ calendar=$cal; name="New Year's Day";    date='2026-01-01 00:00:00.000Z'; recurring=$true  } name "New Year's Day"      | Out-Null
  New-Rec holidays @{ calendar=$cal; name='Memorial Day';      date='2026-05-25 00:00:00.000Z'; recurring=$false } name 'Memorial Day'        | Out-Null
  New-Rec holidays @{ calendar=$cal; name='Independence Day';  date='2026-07-04 00:00:00.000Z'; recurring=$true  } name 'Independence Day'    | Out-Null
  New-Rec holidays @{ calendar=$cal; name='Labor Day';         date='2026-09-07 00:00:00.000Z'; recurring=$false } name 'Labor Day'           | Out-Null
  New-Rec holidays @{ calendar=$cal; name='Thanksgiving';      date='2026-11-26 00:00:00.000Z'; recurring=$false } name 'Thanksgiving'        | Out-Null
  New-Rec holidays @{ calendar=$cal; name='Christmas Day';     date='2026-12-25 00:00:00.000Z'; recurring=$true  } name 'Christmas Day'       | Out-Null
}

Write-Host "`n== Locations =="
$cals = @(); if ($ID['us-federal']) { $cals = @($ID['us-federal']) }
$ID['dc']         = New-Rec locations @{ code='dc';         name='Distribution Center'; timezone='America/Chicago';  fai_suppress=$true; notify_fire=$false; description='Regional distribution and warehouse facility.'; coordinates=@{lat=32.7767; lon=-96.7970}; holiday_calendars=$cals }
$ID['east-office']= New-Rec locations @{ code='east-office';name='East Coast Office';   timezone='America/New_York'; fai_suppress=$true; notify_fire=$false; description='Sales and client services office.';           coordinates=@{lat=40.7128; lon=-74.0060}; holiday_calendars=$cals }

Write-Host "`n== Controllers =="
$ID['ctrl-dc-1']  = New-Rec controllers @{ code='ctrl-dc-1';   name='DC Controller 1';        location=$ID['dc'];          model='kincony-server-mini' }
$ID['ctrl-east-1']= New-Rec controllers @{ code='ctrl-east-1'; name='East Office Controller'; location=$ID['east-office'];  model='kincony-server-mini' }

Write-Host "`n== Areas =="
$ID['dc-warehouse'] = New-Rec areas @{ code='dc-warehouse'; name='DC Warehouse'; location=$ID['dc']; arm='armed'; arm_override=''; auto_arm=''; auto_schedule=''; notify_on_alarm=$true }

Write-Host "`n== Portals =="
# HQ additions (controller ctrl-hq-1; relays 1-2 & inputs 1-4 already in use)
$ID['hq-server-room'] = New-Rec portals @{ code='hq-server-room'; name='HQ Server Room';      type='door'; location=$ID['hq']; controller=$ID['ctrl-hq-1']; posture='secure'; pulse_seconds=5; held_open_seconds=30; reader_address=-1; lock_relay=3; dps_input=7; rex_input=8 }
$ID['hq-east-stair']  = New-Rec portals @{ code='hq-east-stair';  name='HQ East Stairwell';   type='door'; location=$ID['hq']; controller=$ID['ctrl-hq-1']; posture='secure'; pulse_seconds=5; held_open_seconds=30; reader_address=-1; lock_relay=4; dps_input=9; rex_input=10 }
# Distribution Center (ctrl-dc-1)
$ID['dc-main-entrance']= New-Rec portals @{ code='dc-main-entrance'; name='DC Main Entrance';    type='door';      location=$ID['dc']; controller=$ID['ctrl-dc-1']; posture='secure'; pulse_seconds=5; held_open_seconds=30; reader_address=-1; lock_relay=1; dps_input=1; rex_input=2; area=$ID['dc-warehouse']; disarm_on_grant=$true }
$ID['dc-dock-1']       = New-Rec portals @{ code='dc-dock-1';        name='DC Loading Dock 1';   type='door';      location=$ID['dc']; controller=$ID['ctrl-dc-1']; posture='secure'; pulse_seconds=5; held_open_seconds=60; reader_address=-1; lock_relay=2; dps_input=3; rex_input=4; area=$ID['dc-warehouse']; notify_on_alarm=$true }
$ID['dc-dock-2']       = New-Rec portals @{ code='dc-dock-2';        name='DC Loading Dock 2';   type='door';      location=$ID['dc']; controller=$ID['ctrl-dc-1']; posture='secure'; pulse_seconds=5; held_open_seconds=60; reader_address=-1; lock_relay=3; dps_input=5; rex_input=6; area=$ID['dc-warehouse']; notify_on_alarm=$true }
$ID['dc-gate']         = New-Rec portals @{ code='dc-gate';          name='DC Vehicle Gate';     type='gate';      location=$ID['dc']; controller=$ID['ctrl-dc-1']; posture='secure'; pulse_seconds=8; held_open_seconds=120;reader_address=-1; lock_relay=4; dps_input=7; rex_input=8 }
$ID['dc-turnstile']    = New-Rec portals @{ code='dc-turnstile';     name='DC Pedestrian Turnstile'; type='turnstile'; location=$ID['dc']; controller=$ID['ctrl-dc-1']; posture='secure'; pulse_seconds=3; held_open_seconds=15; reader_address=-1; lock_relay=5; dps_input=9; rex_input=10 }
# East Coast Office (ctrl-east-1)
$ID['east-lobby']      = New-Rec portals @{ code='east-lobby';       name='East Lobby';          type='door';      location=$ID['east-office']; controller=$ID['ctrl-east-1']; posture='secure'; pulse_seconds=5; held_open_seconds=30; reader_address=-1; lock_relay=1; dps_input=1; rex_input=2; auto_posture='unlocked'; auto_schedule=$ID['office-hours'] }
$ID['east-elevator']   = New-Rec portals @{ code='east-elevator';    name='East Elevator';       type='elevator';  location=$ID['east-office']; controller=$ID['ctrl-east-1']; posture='secure'; pulse_seconds=5; held_open_seconds=30; reader_address=-1; lock_relay=2; dps_input=3; rex_input=4 }
$ID['east-garage']     = New-Rec portals @{ code='east-garage';      name='East Parking Garage'; type='gate';      location=$ID['east-office']; controller=$ID['ctrl-east-1']; posture='secure'; pulse_seconds=8; held_open_seconds=120;reader_address=-1; lock_relay=3; dps_input=5; rex_input=6 }
$ID['east-server-room']= New-Rec portals @{ code='east-server-room'; name='East Server Room';    type='door';      location=$ID['east-office']; controller=$ID['ctrl-east-1']; posture='secure'; pulse_seconds=5; held_open_seconds=30; reader_address=-1; lock_relay=4; dps_input=7; rex_input=8 }

Write-Host "`n== Aux inputs / outputs =="
New-Rec aux_input  @{ code='dc-motion-1';  name='DC Warehouse Motion'; location=$ID['dc']; controller=$ID['ctrl-dc-1']; input_index=1; point_type='intrusion';  area=$ID['dc-warehouse']; contact='' } | Out-Null
New-Rec aux_input  @{ code='dc-glassbreak';name='DC Glassbreak';       location=$ID['dc']; controller=$ID['ctrl-dc-1']; input_index=2; point_type='tamper_24h'; area=$ID['dc-warehouse']; contact='' } | Out-Null
New-Rec aux_output @{ code='dc-siren';        name='DC Warehouse Siren'; location=$ID['dc'];          controller=$ID['ctrl-dc-1'];   relay_index=6; pulse_seconds=30 } | Out-Null
$ID['east-gate-strike'] = New-Rec aux_output @{ code='east-gate-strike';name='East Gate Strike';   location=$ID['east-office']; controller=$ID['ctrl-east-1']; relay_index=5; pulse_seconds=4  }

Write-Host "`n== Access groups =="
$allPortals = @('lobby-main','lobby-public','hq-server-room','hq-east-stair','dc-main-entrance','dc-dock-1','dc-dock-2','dc-gate','dc-turnstile','east-lobby','east-elevator','east-garage','east-server-room') | ForEach-Object { $ID[$_] } | Where-Object { $_ }
$officeDoors = @('lobby-main','lobby-public','hq-server-room','hq-east-stair','east-lobby','east-elevator','east-server-room') | ForEach-Object { $ID[$_] } | Where-Object { $_ }
$warehouse   = @('dc-main-entrance','dc-dock-1','dc-dock-2','dc-gate','dc-turnstile') | ForEach-Object { $ID[$_] } | Where-Object { $_ }
$serverRooms = @('hq-server-room','east-server-room') | ForEach-Object { $ID[$_] } | Where-Object { $_ }
$gates       = @('dc-gate','east-garage') | ForEach-Object { $ID[$_] } | Where-Object { $_ }
$cleaning    = @('lobby-main','hq-east-stair','east-lobby') | ForEach-Object { $ID[$_] } | Where-Object { $_ }

$ID['ag-all-247']    = New-Rec access_groups @{ code='ag-all-247';    name='All Doors 24/7';            schedule=$ID['always'];          portals=$allPortals }
$ID['ag-office']     = New-Rec access_groups @{ code='ag-office';     name='Office Doors (Office Hours)';schedule=$ID['office-hours'];    portals=$officeDoors }
# Warehouse Access carries the AREA as well as the doors, with both rights: the crew who
# work in there are the people who disarm it on the way in and arm it on the way out.
$ID['ag-warehouse']  = New-Rec access_groups @{ code='ag-warehouse';  name='Warehouse Access';          schedule=$ID['extended-hours'];  portals=$warehouse; areas=@($ID['dc-warehouse']); area_rights=@('arm','disarm') }
$ID['ag-server']     = New-Rec access_groups @{ code='ag-server';     name='Server Rooms';              schedule=$ID['always'];          portals=$serverRooms }
$ID['ag-parking']    = New-Rec access_groups @{ code='ag-parking';    name='Parking & Gates';           schedule=$ID['extended-hours'];  portals=$gates }
# Cleaning Access is the asymmetry worth demonstrating: the crew may ARM the warehouse
# when they finish, and may NOT disarm it. "May lock up but not silence the building" is
# why arm and disarm are separate rights rather than one boolean.
$ID['ag-cleaning']   = New-Rec access_groups @{ code='ag-cleaning';   name='Cleaning Access';           schedule=$ID['cleaning-crew'];   portals=$cleaning; areas=@($ID['dc-warehouse']); area_rights=@('arm') }
# Gates & relays: the vehicle-gate strike as an aux output on a badge, which is the
# ordinary case for this feature (let a delivery in without opening a door).
$ID['ag-relays']     = New-Rec access_groups @{ code='ag-relays';     name='Gate Relays';               schedule=$ID['extended-hours'];  aux_outputs=@($ID['east-gate-strike']) }

Write-Host "`n== Roles =="
$ID['management']     = New-Rec roles @{ code='management';     name='Management';      access_groups=@($ID['ag-all-247']) }
$ID['security']       = New-Rec roles @{ code='security';       name='Security';        access_groups=@($ID['ag-all-247'], $ID['ag-warehouse'], $ID['ag-relays']) }
$ID['facilities']     = New-Rec roles @{ code='facilities';     name='Facilities';      access_groups=@($ID['ag-office'], $ID['ag-parking'], $ID['ag-relays']) }
$ID['it']             = New-Rec roles @{ code='it';             name='IT';              access_groups=@($ID['ag-office'], $ID['ag-server']) }
$ID['warehouse-staff']= New-Rec roles @{ code='warehouse-staff';name='Warehouse Staff'; access_groups=@($ID['ag-warehouse'], $ID['ag-parking']) }
# visitor_preset marks a role as offerable in the visitor mint flow. Without at least
# one, "New Visitor Pass" has nothing to grant — issuing a pass can never hand out more
# than an operator has curated for guests.
$ID['contractor']     = New-Rec roles @{ code='contractor';     name='Contractors';     access_groups=@($ID['ag-cleaning']); visitor_preset=$true }
$ID['visitor-escort'] = New-Rec roles @{ code='visitor-escort'; name='Visitor (Escorted)'; access_groups=@($ID['ag-office']); visitor_preset=$true }

# Enrich the existing Staff role so demo staff reach the office doors too (additive).
if ($ID['staff'] -and $ID['ag-office'] -and $ID['lobby-group']) {
  $payload = @{ access_groups = @($ID['lobby-group'], $ID['ag-office']) } | ConvertTo-Json -Depth 6 -Compress
  $payload | pb collections update roles $ID['staff'] --output json 2>$null | Out-Null
  Write-Host "  ~ roles/staff access_groups -> [lobby-group, ag-office]"
}

Write-Host "`n== Cardholders =="
# `badge` gives this person a sign-in for the badge page. Deliberately a minority: most
# people in a PACS only ever tap a card, and giving everybody a login would put a
# phone-openable surface on people who never asked for one.
#
# `pw` is the demo password an operator would hand over in person. It is what makes the
# badge tier usable with NO SMTP at all — a one-time code is an email, so without a mail
# server it is the only way in. Real installs hand these over face to face and never mail
# them; here it is a known string so the demo can sign in. password_set=true records that
# the holder knows it, which is what makes a later self-service change demand the old one.
$DEMO_BADGE_PW = 'badge-demo-1234'
$people = @(
  @{ ext='EMP-1001'; name='Sarah Chen';        email='sarah.chen@stoneage.example';      role='management';      badge=$true  },
  @{ ext='EMP-1002'; name='Marcus Johnson';    email='marcus.johnson@stoneage.example';  role='security';        badge=$true  },
  @{ ext='EMP-1003'; name='Priya Patel';       email='priya.patel@stoneage.example';     role='it';              badge=$true  },
  @{ ext='EMP-1004'; name='David Kim';         email='david.kim@stoneage.example';       role='facilities'       },
  @{ ext='EMP-1005'; name='Emily Rodriguez';   email='emily.rodriguez@stoneage.example'; role='staff';           badge=$true  },
  @{ ext='EMP-1006'; name='James Wilson';      email='james.wilson@stoneage.example';    role='warehouse-staff'  },
  @{ ext='EMP-1007'; name='Olivia Martinez';   email='olivia.martinez@stoneage.example'; role='staff'            },
  @{ ext='EMP-1008'; name='Robert Taylor';     email='robert.taylor@stoneage.example';   role='security'         },
  @{ ext='EMP-1009'; name='Linda Nguyen';      email='linda.nguyen@stoneage.example';    role='management'       },
  @{ ext='EMP-1010'; name='Carlos Gomez';      email='carlos.gomez@stoneage.example';    role='warehouse-staff'  },
  @{ ext='EMP-1011'; name='Hannah Schmidt';    email='hannah.schmidt@stoneage.example';  role='facilities'       },
  @{ ext='EMP-1012'; name='Tom Burns';         email='tom.burns@stoneage.example';       role='staff'; status='suspended' },
  @{ ext='VND-2001'; name='Night Owl Cleaning';email='dispatch@nightowl.example';        role='contractor'       }
)
foreach ($p in $people) {
  $st = if ($p.status) { $p.status } else { 'active' }
  $rid = $ID[$p.role]
  $d = @{ external_id=$p.ext; name=$p.name; email=$p.email; status=$st; roles=@($rid) }
  if ($p.badge) {
    $d['badge_login']  = $true
    $d['password']     = $DEMO_BADGE_PW
    $d['password_set'] = $true
    # An operator set this password while looking at the person, which is a stronger
    # identity check than clicking a link in an inbox — and marking it verified also
    # keeps PocketBase from randomising the password if they later sign in with a
    # one-time code instead (see badgeapi.bindOTPPasswordPreservation).
    $d['verified']     = $true
  }
  $ID['ch:'+$p.email] = New-Cardholder $d
}

# Cardholders with NO email address at all: the contractors, hourly staff and
# non-person cards every real install carries. They cannot sign in by any method, which
# is exactly why email is optional on this collection even though it is the auth
# identity — requiring it would force a synthetic address onto each of these.
$ID['ch:dock-spare'] = New-Rec cardholders @{
  external_id='CARD-SPARE-1'; name='Loading Dock Spare Card'; status='active'; roles=@($ID['warehouse-staff'])
  password='unused-not-a-sign-in-path'; passwordConfirm='unused-not-a-sign-in-path'
} external_id 'CARD-SPARE-1'
$ID['ch:lockbox'] = New-Rec cardholders @{
  external_id='FD-LOCKBOX'; name='Fire Dept Lockbox'; status='active'; roles=@($ID['security'])
  password='unused-not-a-sign-in-path'; passwordConfirm='unused-not-a-sign-in-path'
} external_id 'FD-LOCKBOX'

Write-Host "`n== Visitors =="
# A visitor is a cardholder with kind='visitor' and a pass that expires — the same
# collection the Cardholders page lists, which is why that page filters them out and
# Visitors shows them on their own. Seeded directly rather than through
# POST /api/badge/visitors so this script needs nothing but the `pb` CLI.
#
# Three states, so the Visitors page has something to show in each: live, expired, and
# revoked. `kind='visitor'` is the one field that must be right — it decides that the QR
# carries the credential VALUE (so a scanner can read it) rather than an inert identifier.
$visitors = @(
  @{ name='Dana Whitfield';  email='dana.whitfield@acme.example';   role='visitor-escort'; state='live'    },
  @{ name='Ibrahim Osei';    email='ibrahim.osei@vendor.example';   role='contractor';     state='expired' },
  @{ name='Grace Lindqvist'; email='grace.lindqvist@acme.example';  role='visitor-escort'; state='revoked' }
)
foreach ($v in $visitors) {
  $ID['ch:'+$v.email] = New-Cardholder @{
    external_id=''; name=$v.name; email=$v.email; status='active'; roles=@($ID[$v.role])
    kind='visitor'; badge_login=$true
    # A real visitor normally gets no password — an emailed one-time code is the whole
    # point of not making someone invent one for a one-day pass. The DEMO gives them the
    # same password as the staff badges, because a demo you cannot sign into without
    # configuring SMTP is not much of a demo, and because the operator-set initial
    # password is itself a real feature: it is what makes the badge tier usable on an
    # install with no mail server (the visitor mint form has a field for it).
    # verified=true because for a real visitor the code round-trip is the only identity
    # check they get.
    verified=$true; password=$DEMO_BADGE_PW; password_set=$true
  }
}

Write-Host "`n== Credentials =="
$creds = @(
  @{ ch='sarah.chen@stoneage.example';      value='CARD-1001'; type='mobile';  label='Sarah Chen - mobile' },
  @{ ch='sarah.chen@stoneage.example';      value='PIN-4823';  type='pin';     label='Sarah Chen - PIN' },
  @{ ch='marcus.johnson@stoneage.example';  value='CARD-1002'; type='wiegand'; label='Marcus Johnson - badge' },
  @{ ch='priya.patel@stoneage.example';     value='CARD-1003'; type='wiegand'; label='Priya Patel - badge' },
  @{ ch='david.kim@stoneage.example';       value='CARD-1004'; type='wiegand'; label='David Kim - badge' },
  @{ ch='emily.rodriguez@stoneage.example'; value='CARD-1005'; type='wiegand'; label='Emily Rodriguez - badge' },
  @{ ch='james.wilson@stoneage.example';    value='CARD-1006'; type='wiegand'; label='James Wilson - badge' },
  @{ ch='olivia.martinez@stoneage.example'; value='CARD-1007'; type='mobile';  label='Olivia Martinez - mobile' },
  @{ ch='robert.taylor@stoneage.example';   value='CARD-1008'; type='wiegand'; label='Robert Taylor - badge' },
  @{ ch='linda.nguyen@stoneage.example';    value='CARD-1009'; type='mobile';  label='Linda Nguyen - mobile' },
  @{ ch='carlos.gomez@stoneage.example';    value='CARD-1010'; type='wiegand'; label='Carlos Gomez - badge' },
  @{ ch='hannah.schmidt@stoneage.example';  value='CARD-1011'; type='wiegand'; label='Hannah Schmidt - badge' },
  @{ ch='tom.burns@stoneage.example';       value='CARD-1012'; type='wiegand'; label='Tom Burns - badge' },
  @{ ch='dispatch@nightowl.example';        value='CARD-2001'; type='wiegand'; label='Night Owl Cleaning - contractor'; valid_from='2026-06-01 00:00:00.000Z'; valid_until='2026-09-30 00:00:00.000Z' }
)
foreach ($c in $creds) {
  $uid = $ID['ch:'+$c.ch]
  if (-not $uid) { Write-Host "  ! credential $($c.value): cardholder not found ($($c.ch))"; continue }
  $st = if ($c.status) { $c.status } else { 'active' }
  $d = @{ value=$c.value; type=$c.type; label=$c.label; status=$st; user=$uid; valid_from=''; valid_until='' }
  if ($c.valid_from)  { $d.valid_from  = $c.valid_from }
  if ($c.valid_until) { $d.valid_until = $c.valid_until }
  New-Rec credentials $d value $c.value | Out-Null
}

# The two no-email cards.
if ($ID['ch:dock-spare']) { New-Rec credentials @{ value='CARD-SPARE-1'; type='wiegand'; label='Loading dock spare'; status='active'; user=$ID['ch:dock-spare']; valid_from=''; valid_until='' } value 'CARD-SPARE-1' | Out-Null }
if ($ID['ch:lockbox'])    { New-Rec credentials @{ value='FD-LOCKBOX';   type='wiegand'; label='Fire dept lockbox';  status='active'; user=$ID['ch:lockbox'];    valid_from=''; valid_until='' } value 'FD-LOCKBOX'   | Out-Null }

Write-Host "`n== Visitor passes (time-bound credentials) =="
# Values are uppercase, V-prefixed and inside the KV key charset
# (policykv.CredentialValuePattern) — a value outside it saves fine and then silently
# never mirrors to the edge, so the pass would look active and open nothing.
function Stamp([int]$days) { return (Get-Date).ToUniversalTime().AddDays($days).ToString("yyyy-MM-dd HH:mm:ss.fff'Z'") }
$passes = @(
  @{ ch='dana.whitfield@acme.example';  value='V-DEMOLIVE0001'; label='Visitor pass'; status='active';  from=(Stamp -1); until=(Stamp 1)  },
  @{ ch='ibrahim.osei@vendor.example';  value='V-DEMOEXPIRED1'; label='Visitor pass'; status='active';  from=(Stamp -9); until=(Stamp -2) },
  @{ ch='grace.lindqvist@acme.example'; value='V-DEMOREVOKED1'; label='Visitor pass'; status='revoked'; from=(Stamp -3); until=(Stamp 3)  }
)
foreach ($v in $passes) {
  $uid = $ID['ch:'+$v.ch]
  if (-not $uid) { Write-Host "  ! visitor pass $($v.value): cardholder not found ($($v.ch))"; continue }
  New-Rec credentials @{ value=$v.value; type='mobile'; label=$v.label; status=$v.status; user=$uid; valid_from=$v.from; valid_until=$v.until } value $v.value | Out-Null
}

Write-Host "`n== Remote unlock (badge page) =="
# allow_remote_unlock is per-door and defaults FALSE, because "may walk through" and "may
# open from anywhere with no presence proof" are different permissions. Two interior doors
# are opted in so the badge page has working buttons; the perimeter and the vehicle gate
# stay closed to it deliberately.
Set-Rec portals $ID['east-lobby']     @{ allow_remote_unlock=$true } 'east-lobby      allow_remote_unlock=true'
Set-Rec portals $ID['hq-east-stair']  @{ allow_remote_unlock=$true } 'hq-east-stair   allow_remote_unlock=true'

# The same split for the other two badge actions. Each is per-record and defaults FALSE:
# an access group deciding WHO may arm is separate from whether it may be done with
# nobody on site. Both are opted in here so the badge Access tab has working buttons.
Set-Rec areas      $ID['dc-warehouse']     @{ allow_remote_arm=$true } 'dc-warehouse    allow_remote_arm=true'
Set-Rec aux_output $ID['east-gate-strike'] @{ allow_remote=$true }     'east-gate-strike allow_remote=true'

# The floor plan on badges: on for the East office only, so the demo shows both halves —
# a site where holders get a plan with their own doors pinned, and sites where they get
# the list. Needs a floorplan image uploaded to the location to actually render.
Set-Rec locations  $ID['east-office'] @{ badge_floorplan=$true } 'east-office     badge_floorplan=true'

Write-Host "`n== Operator's own badge =="
# One human, two records: the console account and the cardholder. Linking them lets that
# operator view their OWN badge from the console's profile menu without signing in twice.
# It grants nothing in either direction — a pointer, not a merge.
#
# The relation lives on the CARDHOLDER because the users collection is self-writable (an
# operator changes their own password there), so the mirror-image field would let any
# operator repoint it and inherit someone else's badge. Uses the fixture's admin account,
# which is the only operator a fresh install has.
$adminId = Get-Id users email 'admin@local.dev'
if ($adminId -and $ID['ch:sarah.chen@stoneage.example']) {
  Set-Rec cardholders $ID['ch:sarah.chen@stoneage.example'] @{ operator=$adminId } 'Sarah Chen      operator=admin@local.dev'
} else {
  Write-Host '  = no admin@local.dev operator found; skipping the badge link'
}

Write-Host "`n== Events (recent activity + unacknowledged alarms) =="
$evtSeeded = Get-Id events credential 'CARD-1001'
if ($evtSeeded) {
  Write-Host "  = demo events already present; skipping"
} else {
  function Ago([int]$min) { return (Get-Date).ToUniversalTime().AddMinutes(-$min).ToString("yyyy-MM-dd HH:mm:ss.fff'Z'") }
  $events = @(
    @{ location='hq';  portal='lobby-main';       type='door';      kind='tap'; credential='CARD-001';  user='Alice Example';     allow=$true;  reason='allow_grant';            min=2 },
    @{ location='east-office';portal='east-lobby';       type='door';      kind='tap'; credential='CARD-1001'; user='Sarah Chen';        allow=$true;  reason='allow_grant';            min=3 },
    @{ location='east-office';portal='east-lobby';       type='door';      kind='tap'; credential='CARD-1005'; user='Emily Rodriguez';   allow=$true;  reason='allow_posture_unlocked'; min=5 },
    @{ location='east-office';portal='east-server-room'; type='door';      kind='tap'; credential='CARD-1007'; user='Olivia Martinez';   allow=$false; reason='deny_no_access';         min=6 },
    @{ location='dc';  portal='dc-main-entrance'; type='door';      kind='tap'; credential='CARD-1006'; user='James Wilson';      allow=$true;  reason='allow_grant';            min=8 },
    @{ location='dc';  portal='dc-dock-1';        type='door';      kind='tap'; credential='CARD-2001'; user='Night Owl Cleaning';allow=$false; reason='deny_schedule_closed';   min=11 },
    @{ location='hq';  portal='hq-server-room';   type='door';      kind='tap'; credential='CARD-1004'; user='David Kim';         allow=$false; reason='deny_no_access';         min=20 },
    @{ location='dc';  portal='dc-turnstile';     type='turnstile'; kind='tap'; credential='CARD-9999'; user='';                  allow=$false; reason='deny_unknown_credential';min=25 },
    @{ location='hq';  portal='lobby-main';       type='door';      kind='tap'; credential='CARD-1012'; user='Tom Burns';         allow=$false; reason='deny_revoked';           min=30 }
  )
  foreach ($e in $events) {
    $d = @{ location=$e.location; portal=$e.portal; type=$e.type; kind=$e.kind; credential=$e.credential; user=$e.user; allow=$e.allow; reason=$e.reason; source='osdp'; ts=(Ago $e.min); payload=@{}; acknowledged=$true }
    $json = $d | ConvertTo-Json -Depth 8 -Compress
    $res = $null; try { $res = $json | pb collections create events --output json 2>$null | ConvertFrom-Json } catch {}
    if ($res -and $res.id) { $script:created++; Write-Host ("  + event tap   {0,-18} {1}" -f $e.portal, $e.reason) }
    else { $script:failed++; $err = $json | pb collections create events --output json 2>&1 | Out-String; Write-Host ("  ! event {0} FAILED: {1}" -f $e.portal, $err.Trim()); break }
  }
  # Unacknowledged alarms (drive the Overview status card + Alarm Console)
  $alarms = @(
    @{ location='dc';  portal='dc-dock-2';     type='door'; payload=@{ type='forced' };                       min=14 },
    @{ location='dc';  portal='dc-warehouse';  type='area'; payload=@{ type='intrusion'; point='dc-motion-1' };min=18 },
    @{ location='hq';  portal='lobby-main';    type='door'; payload=@{ type='held' };                          min=42 }
  )
  foreach ($a in $alarms) {
    $d = @{ location=$a.location; portal=$a.portal; type=$a.type; kind='alarm'; allow=$false; reason=''; ts=(Ago $a.min); payload=$a.payload; acknowledged=$false }
    $json = $d | ConvertTo-Json -Depth 8 -Compress
    $res = $null; try { $res = $json | pb collections create events --output json 2>$null | ConvertFrom-Json } catch {}
    if ($res -and $res.id) { $script:created++; Write-Host ("  + event ALARM {0,-18} {1}" -f $a.portal, $a.payload.type) }
    else { $script:failed++; Write-Host ("  ! alarm event {0} FAILED" -f $a.portal) }
  }
}

Write-Host ("`n== Done.  created={0}  skipped(existing)={1}  failed={2} ==" -f $script:created, $script:skipped, $script:failed)
Write-Host ""
Write-Host "Badge sign-in (http://127.0.0.1:8090/login?as=badge):"
Write-Host ("  staff    sarah.chen@stoneage.example  /  {0}" -f $DEMO_BADGE_PW)
Write-Host ("           marcus.johnson, priya.patel, emily.rodriguez  (same password)")
Write-Host  "  visitor  dana.whitfield@acme.example   -> emailed one-time code (needs SMTP)"
Write-Host  "  fixture  alice@example.com            /  changeme123"
Write-Host ""
Write-Host "Operator console (http://127.0.0.1:8090/login):  admin@local.dev / changeme123"
Write-Host "  That account is linked to Sarah Chen's cardholder, so 'My badge' in the profile menu works."

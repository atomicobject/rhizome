---
type: ExperienceSpec
id: SPEC-9001
summary: "A passenger can book, understand disruptions, and recover an itinerary."
spec-status: active
last-updated: 2026-11-09
---
# Harbor booking and recovery for passengers with long itineraries

## Summary
A synthetic contract with ready, draft, and satisfied stories in authored order.

## Goals
Keep exact itinerary context readable.

## Non-Goals
No live bookings.

## User Stories

### US1 - Reserve a seat with an accessible confirmation

- id:: ^SPEC-9001-US1
- summary:: Passenger outcome A retains the authored source and recorded state.
- status:: ready

#### Acceptance Criteria

- **A confirmation remains readable.** The passenger sees the exact service, travel date, boarding location, assistance instructions, and a deliberately long confirmation narrative that wraps without losing its final phrase: harbor confirmation ends here. ^ae-a-confirmation
  - A nested example is detail, not another direct criterion.
  - A second nested example retains authored order.

  ```text
  service = "Harbor crossing"
  confirmation = "Keep the complete itinerary"
  ```

  verification:: inspect the passenger confirmation
- **A cancellation preserves the itinerary.** Returning from cancellation keeps the original service.

### US2 - Explain a service disruption before booking

- id:: ^SPEC-9001-US2
- summary:: Passenger outcome B retains the authored source and recorded state.
- status:: draft

#### Acceptance Criteria

- **B confirmation remains readable.** The passenger sees the exact service, travel date, boarding location, assistance instructions, and a deliberately long confirmation narrative that wraps without losing its final phrase: harbor confirmation ends here. ^ae-b-confirmation
  - A nested example is detail, not another direct criterion.
  - A second nested example retains authored order.

  ```text
  service = "Harbor crossing"
  confirmation = "Keep the complete itinerary"
  ```

  verification:: inspect the passenger confirmation
- **B cancellation preserves the itinerary.** Returning from cancellation keeps the original service.

### US3 - Recover a saved itinerary across devices

- id:: ^SPEC-9001-US3
- summary:: Passenger outcome C retains the authored source and recorded state.
- status:: ready

#### Acceptance Criteria

- **C confirmation remains readable.** The passenger sees the exact service, travel date, boarding location, assistance instructions, and a deliberately long confirmation narrative that wraps without losing its final phrase: harbor confirmation ends here. ^ae-c-confirmation
  - A nested example is detail, not another direct criterion.
  - A second nested example retains authored order.

  ```text
  service = "Harbor crossing"
  confirmation = "Keep the complete itinerary"
  ```

  verification:: inspect the passenger confirmation
- **C cancellation preserves the itinerary.** Returning from cancellation keeps the original service.

### US4 - Read the published timetable

- id:: ^SPEC-9001-US4
- summary:: Passenger outcome D retains the authored source and recorded state.
- status:: satisfied

#### Acceptance Criteria

- **D confirmation remains readable.** The passenger sees the exact service, travel date, boarding location, assistance instructions, and a deliberately long confirmation narrative that wraps without losing its final phrase: harbor confirmation ends here. ^ae-d-confirmation
  - A nested example is detail, not another direct criterion.
  - A second nested example retains authored order.

  ```text
  service = "Harbor crossing"
  confirmation = "Keep the complete itinerary"
  ```

  verification:: inspect the passenger confirmation
- **D cancellation preserves the itinerary.** Returning from cancellation keeps the original service.

## Requirements
The full normative narrative remains readable. No inferred acceptance or approval.

## Open Questions
No approver is recorded in these synthetic records.

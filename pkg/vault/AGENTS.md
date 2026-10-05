# Test ownership

- `notediscovery.Plan.Classify` owns configured format admission. Rejection cases must use paths that match the tested glob so another condition cannot supply the denial.
- Config tests isolate and restore every process environment key they change, including keys populated by dotenv loading.

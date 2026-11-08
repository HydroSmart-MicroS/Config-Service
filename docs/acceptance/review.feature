# Acceptance specification. Automated evidence: internal/shared/infrastructure/health/dashboard_test.go
@operacion
Feature: Read the local status dashboard snapshot

  Scenario: Authorized or healthy request
    Given Config Service and its monitored components are running locally
    When the client requests GET /status/services
    Then the response status is 200
    And the snapshot contains component readiness and liveness

  Scenario: Boundary or access control
    Given the review environment is available
    When the client sends POST to the read-only route
    Then the response status is 405
    And the Allow header includes GET

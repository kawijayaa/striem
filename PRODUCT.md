# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

The primary users are CTF players investigating prepared telemetry and solving challenge tasks, often under time pressure. Challenge operators provision each deployment but do not manage data through the player interface.

## Product Purpose

Striem gives a CTF team a focused workspace for querying prepared security telemetry, inspecting event evidence, and answering investigation questions to unlock a challenge flag. Success means players can move quickly from a question to a defensible answer without leaving the investigation workspace.

## Positioning

Striem combines a bounded KQL investigation environment with the challenge's task progression in one per-team deployment. Operators prepare JSON, CSV, or Windows EVTX telemetry and questions ahead of time; players query only that prepared evidence through a constrained, server-validated KQL subset.

## Operating Context

Players write and validate KQL, inspect sortable results and raw event JSON, pivot on values and time ranges, save or share hunts, and submit task answers. Progress is shared by everyone using the deployment. The service is deployed per team behind a CTF platform or authenticating reverse proxy.

## Capabilities and Constraints

- Queries have a five-second execution deadline, a 32 KiB source limit, and a 1,000-row result bound.
- The player interface has no ingestion or dataset-management controls.
- Supported prepared inputs are NDJSON, JSON arrays, CSV, Windows EVTX, and gzip-compressed variants.
- Browser-local state includes recent hunts, saved hunts, and answer drafts.
- The interface supports desktop and mobile investigation workflows.
- There is intentionally no built-in authentication.

## Brand Commitments

The product name is Striem. Product language is concise, operational, and specific to investigation work. Preserve established terms including query, results, data sources, fields, tasks, hunts, and flag.

The interface must feel immediately familiar to Microsoft Sentinel users, with Sentinel's KQL-first Advanced Hunting workflow as the primary interaction reference. Use category-standard SIEM conventions at full fidelity rather than introducing decorative novelty that slows experienced players.

## Evidence on Hand

The repository contains the working product interface, API, automated tests, an example challenge manifest, and a screenshot at `screenshot.png`. No testimonials, customer claims, or external performance claims are available and future work must not fabricate them.

## Product Principles

- Optimize first for fast investigation by CTF players.
- Keep querying, evidence inspection, and pivots in one coherent workspace.
- Preserve the CTF task progression and flag-unlock flow.
- Expose bounded, understandable behavior instead of pretending to support unrestricted KQL.
- Keep operator provisioning separate from the player experience.

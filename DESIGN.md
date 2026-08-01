---
name: Striem
description: A composed dark Sentinel-native workbench for focused KQL investigation.
colors:
  canvas-near-black: "#201f1e"
  editor-well: "#1f1e1d"
  input-well: "#1b1a19"
  work-plane: "#252423"
  command-plane: "#2d2c2b"
  raised-plane: "#323130"
  hover-plane: "#3b3a39"
  divider: "#3b3a39"
  divider-strong: "#5a5856"
  field-border: "#6b6967"
  text-primary: "#f3f2f1"
  text-secondary: "#d2d0ce"
  text-muted: "#b3b0ad"
  text-quiet: "#a19f9d"
  enterprise-header: "#111827"
  enterprise-header-rule: "#344154"
  task-plane: "#172a3a"
  task-divider: "#315c7d"
  fluent-blue: "#75baf2"
  fluent-blue-hover: "#9fd0f5"
  fluent-blue-active: "#5ca9e8"
  selected-blue: "#263f55"
  selection-blue: "#384b5e"
  active-line-blue: "#263746"
  data-teal: "#6fd3d6"
  success-green: "#8fd18f"
  danger-red: "#f1707b"
  syntax-orange: "#f5b383"
  syntax-violet: "#c9a7eb"
typography:
  title:
    fontFamily: "Segoe UI Variable Text, Segoe UI, system-ui, sans-serif"
    fontSize: "13px"
    fontWeight: 600
    lineHeight: 1.3
  body:
    fontFamily: "Segoe UI Variable Text, Segoe UI, system-ui, sans-serif"
    fontSize: "14px"
    fontWeight: 400
    lineHeight: 1.45
  label:
    fontFamily: "Segoe UI Variable Text, Segoe UI, system-ui, sans-serif"
    fontSize: "12px"
    fontWeight: 500
    lineHeight: 1
  compact:
    fontFamily: "Segoe UI Variable Text, Segoe UI, system-ui, sans-serif"
    fontSize: "11px"
    fontWeight: 400
    lineHeight: 1.4
  mono:
    fontFamily: "IBM Plex Mono, Cascadia Mono, Consolas, monospace"
    fontSize: "12px"
    fontWeight: 400
    lineHeight: 1.5
  query:
    fontFamily: "IBM Plex Mono, Cascadia Mono, Consolas, monospace"
    fontSize: "14px"
    fontWeight: 400
    lineHeight: 1.75
  mobile-input:
    fontFamily: "Segoe UI Variable Text, Segoe UI, system-ui, sans-serif"
    fontSize: "16px"
    fontWeight: 400
    lineHeight: 1
rounded:
  square: "0"
  control: "2px"
  floating: "4px"
  count: "9px"
spacing:
  micro: "4px"
  compact: "8px"
  standard: "12px"
  roomy: "16px"
  section: "24px"
components:
  button-primary:
    backgroundColor: "{colors.fluent-blue}"
    textColor: "{colors.enterprise-header}"
    typography: "{typography.label}"
    rounded: "{rounded.control}"
    padding: "0 13px"
    height: "32px"
  button-primary-hover:
    backgroundColor: "{colors.fluent-blue-hover}"
    textColor: "{colors.enterprise-header}"
    typography: "{typography.label}"
    rounded: "{rounded.control}"
    padding: "0 13px"
    height: "32px"
  button-secondary:
    backgroundColor: "{colors.command-plane}"
    textColor: "{colors.text-primary}"
    typography: "{typography.label}"
    rounded: "{rounded.control}"
    padding: "0 10px"
    height: "30px"
  input:
    backgroundColor: "{colors.input-well}"
    textColor: "{colors.text-primary}"
    typography: "{typography.body}"
    rounded: "{rounded.control}"
    padding: "0 10px"
    height: "36px"
  panel:
    backgroundColor: "{colors.work-plane}"
    textColor: "{colors.text-primary}"
    rounded: "{rounded.square}"
    padding: "{spacing.standard}"
---

# Design System: Striem

## Overview

**Creative North Star: "The Dark Investigation Workbench"**

Striem is a compact Sentinel-native hunting station in a deliberately designed dark operating theme. A near-black canvas and stepped charcoal work planes keep the task, schema, KQL editor, and results visually connected, while light Fluent blue marks commands and active state with enterprise restraint. This is not a mechanical inversion of a light palette and not a neon SOC aesthetic: contrast, syntax, semantics, and depth have each been composed for sustained dark-mode investigation.

The interaction grammar follows the category canon of Microsoft Sentinel and Defender Advanced Hunting so experienced analysts can work by recognition. Striem remains an independent product: it is not affiliated with Microsoft, and its interface must not reproduce Microsoft trademarks, logos, product marks, or proprietary brand assets.

**Key Characteristics:**
- Persistent 286px investigation sidebar at left on desktop, with Fields, Tasks, and Hunts as its top-level tabs.
- Hunts contains Saved and Recent query lists; the flexible right side keeps KQL above one uninterrupted Results plane.
- Dark enterprise header followed immediately by a blue-charcoal active task strip.
- Segoe UI for interface language and IBM Plex Mono for queries and inspectable data.
- Near-black wells, stepped charcoal planes, and one-pixel cool gray dividers instead of card gaps.
- Light Fluent blue reserved for commands, focus, selection, active tabs, and KQL keywords.
- Lifted green, teal, orange, red, and violet accents that remain legible without glowing.
- Query, Results, and Investigate views on mobile, with Run fixed at the lower right.

## Colors

The palette is an enterprise-dark composition: warm near-black and charcoal planes establish depth, cool gray dividers preserve structure, and lifted chromatic roles remain readable without becoming luminous decoration.

### Primary
- **Fluent Command Blue** (`fluent-blue`): Run and validation commands, focus indicators, active tab rules, selected-state edges, numeric emphasis, and KQL keywords.
- **Fluent Hover / Active Blue** (`fluent-blue-hover`, `fluent-blue-active`): Brighter hover feedback and pressed or timeline states; neither is a resting surface color.
- **Selected / Selection / Active-Line Blue** (`selected-blue`, `selection-blue`, `active-line-blue`): Distinct blue-charcoal fills for selected rows, text selection, and the current editor line.
- **Task Plane / Task Divider** (`task-plane`, `task-divider`): Persistent challenge context directly under the header, visibly related to blue without becoming a primary-action fill.

### Secondary
- **Data Teal** (`data-teal`): Dynamic types, raw values, and inspectable data syntax.
- **Success Green** (`success-green`): Solved tasks, accepted answers, JSON strings, and the unlocked flag.

### Tertiary
- **Danger Red** (`danger-red`): Query errors, incorrect answers, destructive context, and JSON failure states.
- **Syntax Orange** (`syntax-orange`): Numbers and boolean literals in KQL and raw event data.
- **Syntax Violet** (`syntax-violet`): KQL operators, keeping operator structure distinct from command-blue keywords.

### Neutral
- **Near-Black Canvas** (`canvas-near-black`): Page ground and mobile workspace background.
- **Editor / Input Wells** (`editor-well`, `input-well`): Recessed query, raw-data, and field-entry surfaces.
- **Work / Command / Raised / Hover Planes** (`work-plane`, `command-plane`, `raised-plane`, `hover-plane`): The stepped charcoal hierarchy for persistent panels, toolbars, controls, overlays, and hover feedback.
- **Divider / Strong Divider / Field Border** (`divider`, `divider-strong`, `field-border`): One-pixel region boundaries, table and editor boundaries, and resting field strokes.
- **Primary / Secondary / Muted / Quiet Text** (`text-primary`, `text-secondary`, `text-muted`, `text-quiet`): Four stable levels for headings, content, guidance, and technical metadata.
- **Enterprise Header / Rule** (`enterprise-header`, `enterprise-header-rule`): The navy-black application bar and its cool structural boundary.

### Named Rules

**The Designed Dark Rule.** Compose every role for dark operation; never derive this system by inverting a light screen.

**The Command Color Rule.** Fluent blue means action or active state; never flood a resting work plane with it.

**The Category, Not the Brand Rule.** Use Sentinel and Defender Advanced Hunting conventions without reproducing Microsoft marks or implying Microsoft affiliation.

## Typography

**Display Font:** Segoe UI Variable Text (with Segoe UI and system UI fallbacks)

**Body Font:** Segoe UI Variable Text (with Segoe UI and system UI fallbacks)

**Label/Mono Font:** IBM Plex Mono (with Cascadia Mono and Consolas fallbacks)

**Character:** Segoe UI makes the shell familiar, compact, and operational. IBM Plex Mono gives KQL, field paths, timestamps, result values, and raw JSON a precise data voice without turning the application into terminal cosplay.

### Hierarchy
- **Title** (600, 13px, 1.3): Active task, panel, dialog, and important empty-state headings.
- **Body** (400, 14px, 1.45): General controls, investigation guidance, and task prose.
- **Label** (500-600, 12px, 1): Buttons, tabs, table headers, and compact commands in sentence case.
- **Compact** (400-600, 10-12px, 1.4): Header context, source metadata, counts, field types, and query status.
- **Query** (400, 14px, 1.75): KQL editor content with clear line rhythm inside the dense shell.
- **Mono** (400-600, 10-12px, 1.5): Field paths, result values, timestamps, raw JSON, and technical metadata.
- **Mobile Input** (400, 16px, 1): Narrow-screen answer entry sized to avoid browser zoom.

### Named Rules

**The Two Voices Rule.** Use Segoe UI for interface language and IBM Plex Mono only for query or inspectable data.

## Layout

Above 900px, the workspace is a connected two-column instrument: a fixed 286px investigation sidebar occupies the left edge while the flexible right side stacks a minimum 186px KQL editor above a single uninterrupted Results plane. The sidebar and primary plane meet directly with no card gutters. The 48px application header and minimum 64px task strip keep product context and the current hypothesis ahead of the workbench.

At 900px and below, Query, Results, and Investigate become mutually exclusive views under the task strip. At 600px and below, the interface preserves the same order while increasing important touch controls to at least 42px, translating tables into vertical result records, truncating task copy where required, and fixing Run at the lower right. The compact spacing rhythm uses 4px, 8px, 12px, 16px, and 24px steps; toolbars and dividers provide cadence instead of ornamental whitespace.

## Elevation & Depth

Persistent workspace regions are flat and connected. Depth comes from the tonal sequence between near-black canvas, recessed wells, work planes, and raised charcoal controls, reinforced by one-pixel cool gray rules. Shadows are reserved for temporary layers, dialogs, toasts, and the mobile Run command.

### Shadow Vocabulary
- **Floating Layer** (`0 8px 24px rgba(0, 0, 0, .38)`): Code completion and compact temporary overlays.
- **Dialog** (`0 12px 36px rgba(0, 0, 0, .48)`): Modal separation over a dark neutral backdrop.
- **Toast** (`0 8px 24px rgba(0, 0, 0, .48)`): Temporary system feedback.
- **Mobile Command** (`0 6px 18px rgba(0, 0, 0, .45)`): The fixed Run action on narrow screens.

### Named Rules

**The Tonal Work Plane Rule.** Persistent panels never float; use stepped charcoal and cool one-pixel rules, reserving shadows for temporary UI.

## Shapes

The workspace is rectilinear. Joined panels and tables use square corners; buttons, fields, state tags, and row actions use a restrained 2px radius; dialogs may use 4px. Count indicators alone use a 9px capsule. Selected rows retain their silhouette and gain a two-pixel Fluent-blue inset edge. Borders are structural, never decorative outlines around disconnected cards.

## Components

### Buttons
- **Shape:** Compact rectangles with 2px corners; primary controls are 32px high and secondary controls are 30px high on desktop.
- **Primary:** Light Fluent blue with near-black text and 13px horizontal padding; reserve it for Run, Check answer, Save, and immediate dialog actions.
- **Hover / Focus:** Brighten to the established hover blue, then use the active blue when pressed; focus uses a crisp two-pixel blue outline without glow or lift.
- **Secondary:** Raised charcoal fill, light text, and a cool gray border; hover steps one charcoal level lighter with a stronger border.

### Cards / Containers
- **Corner Style:** Square for connected work planes and 2px only for bounded task, completion, or status states.
- **Background:** Work-plane charcoal for persistent regions, command charcoal for toolbars and headers, near-black for recessed editors and inputs, and blue-charcoal for active context.
- **Shadow Strategy:** None for persistent workspace regions; temporary layers follow the elevation vocabulary.
- **Border:** One-pixel cool gray rules, with blue, green, or red introduced only by state.
- **Internal Padding:** Usually 8-12px, reaching 16px in dialogs and spacious empty states.

### Inputs / Fields
- **Style:** Near-black fill, one-pixel field-gray border, 2px corners, and Segoe UI text; query and raw-data fields use IBM Plex Mono.
- **Focus:** Fluent-blue border plus a two-pixel inset bottom edge; never add ambient focus glow.
- **Error / Disabled:** Lifted red carries errors; disabled controls recede to charcoal with low-contrast gray text and no false affordance.

### Navigation

Navigation uses compact sentence-case Segoe UI, muted text at rest, and a two-pixel Fluent-blue underline with primary text for selection. Desktop places Fields, Tasks, and Hunts in the investigation sidebar. Hunts switches between Saved and Recent query lists. Mobile promotes Query, Results, and Investigate to the primary view switcher while retaining the task strip above it; Hunts remains available inside Investigate.

### Task Strip

The active task is a blue-charcoal horizontal strip directly below the enterprise header. It keeps progress, title, prompt, task access, answer entry, and validation in one persistent row on desktop, then wraps the answer form below the task context on narrow screens.

### Investigation Sidebar

The 286px desktop sidebar contains three equal-width tabs: Fields, Tasks, and Hunts. Fields combines source selection, field filtering, and field insertion; Tasks lists challenge progression; Hunts contains Saved and Recent query lists. Source rows and field groups step between work and command planes with one-pixel separators. The active source uses a blue-charcoal fill and two-pixel Fluent-blue inset edge. Field paths remain monospace; labels, types, and commands remain Segoe UI.

### Query And Results

KQL and Results are the visual and functional primary. The editor sits in a recessed near-black well with a charcoal line-number gutter, blue-charcoal active line, restrained multi-hue syntax, and IBM Plex Mono. Results remain one uninterrupted plane beneath the editor rather than splitting into auxiliary regions. Its sticky command-plane header, dense 12px mono cells, one-pixel row rules, blue-charcoal selection, and blue numeric emphasis preserve scan speed; mobile translates the hierarchy into stacked result records rather than shrinking the table.

## Do's and Don'ts

### Do:
- **Do** treat dark mode as the designed operating theme, with role-specific contrast and tonal depth.
- **Do** keep task, schema, KQL, and results visually connected as one investigation workbench.
- **Do** use light Fluent blue for commands, focus, selection, active tabs, and KQL keywords.
- **Do** use stepped charcoal planes and cool gray dividers to express persistent structure.
- **Do** use Segoe UI for interface language and IBM Plex Mono for query and inspectable data.
- **Do** retain Fields, Tasks, and Hunts in the desktop sidebar, with Saved and Recent inside Hunts.
- **Do** retain Query, Results, and Investigate on mobile, with Hunts inside Investigate.
- **Do** keep Results as one uninterrupted primary plane.

### Don't:
- **Don't** mechanically invert a light palette or introduce neon SOC glow, cyberpunk gradients, or terminal cosplay.
- **Don't** use large radii, floating card stacks, ambient accent glows, or shadows on persistent panels.
- **Don't** let semantic green, teal, orange, red, or violet compete with Fluent-blue commands.
- **Don't** let supporting controls compete with Run, Check answer, or active KQL state.
- **Don't** reproduce Microsoft trademarks, logos, product marks, or imply that Striem is affiliated with Microsoft.
- **Don't** trade dense investigation context for ornamental whitespace.

# AgentBoard Skill

AgentBoard is a live dashboard that updates in real time as you write data to it.
You have tools to read and write data, create and edit pages, and discover available components.

## Workflow

When the user asks you to track something or build a dashboard:

1. Call `agentboard_list_components` to see what visualization components are available.
2. Call `agentboard_get_data_schema` to see what data already exists.
3. Decide what data to track and what components to use.
4. Use `agentboard_set` to populate initial data.
5. Use `agentboard_write_page` to create or update the MDX page.

## Data Model

Data is a key-value store. Keys are dotted paths like `sentry.issues` or `analytics.dau`.
Values are any valid JSON — numbers, strings, objects, arrays.

Collections are arrays of objects with an `id` field. You can upsert, merge, and delete individual items.

## MDX Syntax

Pages are written in MDX — markdown with embedded components.

Use JSX interpolation for inline text:
```
Current users: {data.analytics.dau}
```

Use components with `source` prop for visualizations:
```
<Metric source="analytics.dau" label="Daily Active Users" />
```

## Available Components

- `<Metric>` — A single large number with optional trend
- `<Status>` — State indicator (running/passing/failing/waiting/stale)
- `<Progress>` — Progress bar from {value, max}
- `<Table>` — Auto-column table from array of objects
- `<Chart>` — Bar, pie, donut, or horizontal bar chart
- `<TimeSeries>` — Line or bar chart over time
- `<Log>` — Append-only text log
- `<List>` — Ordered/unordered list with optional status badges
- `<Kanban>` — Non-interactive kanban board grouped by a field

## Example

User says: "Track my daily coffee intake."

1. Set initial data:
```
agentboard_set("coffee.today", 0)
agentboard_set("coffee.history", [])
```

2. Write a page:
```
agentboard_write_page("index.md", `
# Coffee Tracker

Today: <Metric source="coffee.today" label="Cups" />

<TimeSeries source="coffee.history" x="date" y="cups" />
`)
```

3. Tell the user the dashboard is at http://localhost:3000

## Updating Data

When the user says something changed:
- Use `agentboard_set` to replace a value
- Use `agentboard_merge` to update specific fields of an object
- Use `agentboard_append` to add items to an array/log

The dashboard updates live — no refresh needed.

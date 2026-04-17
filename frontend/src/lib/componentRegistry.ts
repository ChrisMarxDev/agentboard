import type { ComponentType } from 'react'
import { Metric } from '../components/builtin/Metric'
import { Status } from '../components/builtin/Status'
import { Progress } from '../components/builtin/Progress'
import { Table } from '../components/builtin/Table'
import { Chart } from '../components/builtin/Chart'
import { TimeSeries } from '../components/builtin/TimeSeries'
import { Log } from '../components/builtin/Log'
import { List } from '../components/builtin/List'
import { Kanban } from '../components/builtin/Kanban'
import { Deck } from '../components/builtin/Deck'
import { Card } from '../components/builtin/Card'

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type AnyComponent = ComponentType<any>

const registry = new Map<string, AnyComponent>()

// Register built-in components
registry.set('Metric', Metric)
registry.set('Status', Status)
registry.set('Progress', Progress)
registry.set('Table', Table)
registry.set('Chart', Chart)
registry.set('TimeSeries', TimeSeries)
registry.set('Log', Log)
registry.set('List', List)
registry.set('Kanban', Kanban)
registry.set('Deck', Deck)
registry.set('Card', Card)

export function getComponents(): Record<string, AnyComponent> {
  return Object.fromEntries(registry)
}

export function registerComponent(name: string, component: AnyComponent) {
  registry.set(name, component)
}

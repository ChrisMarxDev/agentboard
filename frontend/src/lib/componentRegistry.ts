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
import { Stack } from '../components/builtin/Stack'
import { Markdown } from '../components/builtin/Markdown'
import { Badge } from '../components/builtin/Badge'
import { Counter } from '../components/builtin/Counter'
import { Code } from '../components/builtin/Code'
import { Mermaid } from '../components/builtin/Mermaid'
import { Image } from '../components/builtin/Image'
import { File as FileComponent } from '../components/builtin/File'
import { Errors } from '../components/builtin/Errors'
import { ApiList } from '../components/builtin/ApiList'
import { Mention } from '../components/builtin/Mention'
import { TeamRoster } from '../components/builtin/TeamRoster'
import { RichText } from '../components/builtin/RichText'
import { SkillInstall } from '../components/builtin/SkillInstall'
import { Button } from '../components/builtin/Button'
import { Sheet } from '../components/builtin/Sheet'
import { Inbox } from '../components/builtin/Inbox'
import { V2Display } from '../components/builtin/V2Display'

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
registry.set('Stack', Stack)
registry.set('Markdown', Markdown)
registry.set('Badge', Badge)
registry.set('Counter', Counter)
registry.set('Code', Code)
registry.set('Mermaid', Mermaid)
registry.set('Image', Image)
registry.set('File', FileComponent)
registry.set('Errors', Errors)
registry.set('ApiList', ApiList)
registry.set('Mention', Mention)
registry.set('TeamRoster', TeamRoster)
registry.set('RichText', RichText)
registry.set('SkillInstall', SkillInstall)
registry.set('Button', Button)
registry.set('Sheet', Sheet)
registry.set('Inbox', Inbox)
registry.set('V2Display', V2Display)

export function getComponents(): Record<string, AnyComponent> {
  return Object.fromEntries(registry)
}

export function registerComponent(name: string, component: AnyComponent) {
  registry.set(name, component)
}

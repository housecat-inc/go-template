---
name: git
description: UX workflow — Design standards, ASCII wireframes, and tempui / templui-pro tools
---

When designing a new UX we want to do rapid design iteration, by scanning existing and reusable components, then with planning with ASCII wireframes, then with pages with simple service stubs.

DO NOT CODE before planning with ASCII wireframes with the developer. DO NOT CODE complex database and services without confirmation. Both are a waste of time until we're more sure we're building the right thing.

1. Scan TemplUI Pro blocks and TemplUI components. Build with templ fragments remixed from these.
2. Plan with ASCII wireframes. Design with elements remixed from these.
3. Build pages with simple service stubs if possible first.

## Design System

Top bar: Buttons like "hide sidebar", "back", "forward". Tabs like "Chat", "Work", "Code".
Side bar: Lists like like "Tools" (search, chats, projects, documents) and "Recents".
Page bar: Breadcrumbs like "Projects" > "Party" (dropdown to star, rename, delete); Buttons like "share".
Main: Single page or split chat + page.
Page: Content like projects list and project overview.

```
┌─────────────────────────────────────────────────┐
│[▯][◀][▶]          [tab1][tab2]                  │
├──────────┬──────────────────────────────────────┤
│          │Breadcrumb / [Dropdown ▽]       [◹][+]│
│          ├──────────────────────────────────────┤
│          │                                      │
│          │                                      │
│          │                                      │
│ Side bar │                                      │
│          │                 Page                 │
│          │                                      │
│          │                                      │
│          │                                      │
│          │                                      │
│          │                                      │
└──────────┴──────────────────────────────────────┘
```

```
┌─────────────────────────────────────────────────┐
│[▯][◀][▶]          [Work][Code]                  │
├──────────┬──────────────────────────────────────┤
│          │Chats / [Hello ▽].              [◹][+]│
│          ├──────────────────┬───────────────────┤
│          │                  │Doc                │
│          │               hi │                   │
│          │hello             │Paragraphs         │
│ Side bar │                  │                   │
│          │                  │- Lists            │
│          ├──────────────────┤                   │
│          │ [btn][btn][btn]  │Tables             │
│          │┌─────────────┐   │ ┌───┬───┬───┬───┐ │
│          ││             │   │ ├───┼───┼───┼───┤ │
│          │└─────────────┘[▲]│ └───┴───┴───┴───┘ │
└──────────┴──────────────────┴───────────────────┘
```

## Templui

Base full pages and complex blocks on templui-pro. See `templui-pro.md` for block index and instructions to get the repo.

Reuse small components from tempui:

```bash
go install github.com/templui/templui/cmd/templui@latest
templui --help

Available components in registry (fetched from ref 'v1.5.0'):
  - accordion      : Collapsible accordion component.
  - alert          : Alert component for messages and notifications.
  - aspectratio    : Container that maintains aspect ratio.
  - avatar         : Avatar component for user profiles.
  - badge          : Badge component for labels and status indicators.
  - breadcrumb     : Breadcrumb navigation component.
  - button         : Button component with multiple variants.
  - calendar       : Calendar component for date selection.
  - card           : Card container component.
  - carousel       : Carousel component with navigation controls.
  - chart          : Chart components for data visualization.
  - checkbox       : Checkbox input component.
  - collapsible    : Collapsible container component.
  - code           : Syntax-highlighted code block component.
  - copybutton     : Copy to clipboard button component.
  - datepicker     : Date picker component combining input and calendar.
  - sheet          : Slide-out panel component (drawer).
  - dropdown       : Dropdown menu component.
  - form           : Form container with validation support.
  - icon           : SVG icon component library.
  - input          : Text input component.
  - inputotp       : One-time password input component.
  - label          : Form label component.
  - dialog         : Modal dialog component.
  - pagination     : Pagination component for lists and tables.
  - popover        : Floating popover component.
  - progress       : Progress bar component.
  - radio          : Radio button group component.
  - rating         : Star rating input component.
  - selectbox      : Searchable select component.
  - separator      : Visual divider between content sections.
  - sidebar        : Collapsible sidebar component for app layouts.
  - skeleton       : Skeleton loading placeholder.
  - slider         : Slider input component.
  - switch         : Toggle switch component.
  - table          : Table component for displaying data.
  - tabs           : Tabbed interface component.
  - tagsinput      : Tags input component.
  - textarea       : Multi-line text input component.
  - timepicker     : Time picker component.
  - toast          : Toast notification component.
  - tooltip        : Tooltip component for additional context.
```

## Example

When planning a chat tool:

- Read templui-pro.md and find `blocks/ai/` and see `blocks/chat/`
- Browse https://pro.templui.io/blocks, https://pro.templui.io/blocks/ai, https://pro.templui.io/blocks/chat
- Run `templui list` and see breadcrumb, button, skeleton, textarea
- Make some ASCII wireframes and discuss with the developer
- Copy templui-pro files, install templ components, and build

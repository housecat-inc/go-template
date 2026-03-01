---
name: ux
description: UX workflow — Design standards, ASCII wireframes, and tempui / templui-pro tools
---

When designing a new UX we want to do rapid design iteration, by scanning existing and reusable components, then with planning with ASCII wireframes, then as pages with simple service stubs.

DO NOT CODE before planning with ASCII wireframes with the developer. DO NOT CODE complex database and services without confirmation. We are rapidly iterating to make sure we're building the right thing.

1. Scan TemplUI Pro blocks and TemplUI components. Build with templ fragments remixed from these.
2. Plan with ASCII wireframes. Design with elements remixed from these.
3. Build pages with an in-memory data store first, using integers for IDs.

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

## CRUD

### Conventions

Use authenticated routes and a user_id. Save CRUD activities through `db/dbgen/activities.sql.go` with display-friendly values like user email in `metadata`.

Use `button.TypeSubmit` buttons for buttons that submit a form. templui buttons default to type="button".

Render timestamps as relative ("just now", "5 minutes ago", "1 hour ago") with a tooltip with the date time including user's local timezone.

### List

Reference this block: https://github.com/housecat-inc/templui-pro/blob/main/blocks/account/team_management_001.templ

List page (`/members`), button to create new (`/members/new`), optional summary cards, items cart ("Team Members"), item cards ("Member") with a dropdown menu for controls ("Update", "Delete").

Add activities feed if applicable.

### Create / Update

Reference this block: https://github.com/housecat-inc/templui-pro/blob/main/blocks/profile/profile_edit_001.templ

Create page (`/members/new`), centered card with form and groups of form items ("Personal Info", "Contact Details", etc.), centered action buttons ("Cancel", "Create Member").

Update page (`/members/:id/edit`) is similar with different action buttons ("Cancel", "Save Changes").

### Get

Reference this block: https://github.com/housecat-inc/templui-pro/blob/main/blocks/profile/profile_overview_001.templ

View page (`/members/:id`), heading with name and other key info, buttons to edit (`/members/:id/edit`) and archive (dialog), cards for infomation ("Personal Info", "Contact Details", etc.) and stats ("Pro"), card for recent activities.

Add activities feed if applicable.

### Archive

Reference the templui `dialog` components: `ui/components/dialog`

Dialog header with action info ("Confirm $Action: $ObjectName"), description with additional context ("This will..."), cancel button and destructive action button ("Archive")

We generally prefer to soft "archive" and "trash" than a hard "delete".

```
┌─────────────────────────────────────────┐
│Confirm Archive: John Doe             [x]│
│                                         │
│This will archive the John Doe user. You │
│can unarchive or trash later.            │
│                                         │
│                       [Cancel] [Archive]│
└─────────────────────────────────────────┘
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
- Copy templui-pro files, install templ components, and build pages with service stubs

Then use the browser skill to review. For every page:

- Test every menu and button
- Look for horizontal and vertical alignment issues

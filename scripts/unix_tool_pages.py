# SPDX-License-Identifier: GPL-2.0-or-later
# Copyright (C) 2026 Abhishek Sengar

import json
from html import escape

SECTION_NAME = "tools"
NAVIGATION_TITLE = "Unix tools"
OVERVIEW_LABEL = "Overview"
EXTERNAL_LINK_ATTRIBUTES = 'target="_blank" rel="noopener"'


def load_tool_pages(source_path):
    catalogue = json.loads(source_path.read_text())
    index_settings = catalogue["index"]
    tools = catalogue["tools"]
    categories = catalogue["categories"]
    base_path = index_settings["path"]
    trail = [[index_settings["nav_label"], base_path]]
    pages = [build_index_page(index_settings, categories, tools)]
    pages.extend(build_tool_page(tool, base_path, trail, tools) for tool in tools)
    return pages, build_navigation_group(base_path, tools)


def tool_path(base_path, tool):
    return f"{base_path}{tool['name']}/"


def build_navigation_group(base_path, tools):
    items = [[base_path, OVERVIEW_LABEL]]
    items.extend([tool_path(base_path, tool), tool["name"]] for tool in tools)
    return {"title": NAVIGATION_TITLE, "collapsible": True, "section": SECTION_NAME, "items": items}


def build_index_page(index_settings, categories, tools):
    return {
        "path": index_settings["path"],
        "layout": "doc",
        "section": SECTION_NAME,
        "trail": [],
        "nav_label": index_settings["nav_label"],
        "priority": "0.8",
        "title": index_settings["title"],
        "description": index_settings["description"],
        "keywords": index_settings["keywords"],
        "body": render_index_body(index_settings, categories, tools),
    }


def build_tool_page(tool, base_path, trail, tools):
    name = tool["name"]
    return {
        "path": tool_path(base_path, tool),
        "layout": "doc",
        "section": SECTION_NAME,
        "trail": trail,
        "nav_label": name,
        "priority": "0.6",
        "title": f"{name} command: {tool['headline']}, with examples | sqx docs",
        "description": f"{tool['summary']} Common {name} options, copy-paste examples, and how to use {name} with sqx.",
        "keywords": [f"{name} command", f"{name} examples", f"{name} linux", f"{name} macos", f"how to use {name}",
                     f"{name} tutorial", f"{name} options"],
        "body": render_tool_body(tool, base_path, tools),
    }


def render_index_body(index_settings, categories, tools):
    sections = [
        f'<h1>Unix and Linux tools</h1>\n<p class="lead">{escape(index_settings["lead"])}</p>',
        render_platform_note(),
    ]
    for category in categories:
        members = [tool for tool in tools if tool["category"] == category["id"]]
        cards = "".join(render_tool_card(index_settings["path"], tool) for tool in members)
        sections.append(
            f'<h2 id="{category["id"]}">{escape(category["title"])}</h2>\n'
            f'<p>{escape(category["summary"])}</p>\n<div class="tool-grid">{cards}</div>'
        )
    return "\n\n".join(sections)


def render_platform_note():
    return (
        '<div class="callout"><p><strong>Linux or macOS?</strong> Linux usually ships the GNU versions of these '
        "tools and macOS ships the BSD versions. The everyday options on these pages work on both; where they "
        "differ, the page says so. Run <code>man &lt;tool&gt;</code> to read the manual for the version you "
        "have.</p></div>"
    )


def render_tool_card(base_path, tool):
    return (
        f'<a class="tool-card" href="/{tool_path(base_path, tool)}">'
        f'<code class="tool-card-name">{escape(tool["name"])}</code>'
        f'<span>{escape(tool["summary"])}</span></a>'
    )


def render_tool_body(tool, base_path, tools):
    parts = [
        render_tool_header(tool),
        render_syntax(tool),
        render_options(tool),
        render_examples(tool),
        render_sqx_pairings(tool),
        render_tips(tool),
        render_learn_more(tool),
        render_related_tools(tool, base_path, tools),
    ]
    return "\n\n".join(part for part in parts if part)


def render_tool_header(tool):
    return (
        f'<h1><code class="tool-title">{escape(tool["name"])}</code> {escape(tool["headline"])}</h1>\n'
        f'<p class="lead">{escape(tool["description"])}</p>\n'
        f'<p class="tool-availability"><span class="badge">Availability</span> {escape(tool["availability"])}</p>'
    )


def render_syntax(tool):
    return f'<h2 id="syntax">Syntax</h2>\n<pre><code>{escape(tool["syntax"])}</code></pre>'


def render_options(tool):
    rows = "".join(
        f"<tr><td><code>{escape(option)}</code></td><td>{escape(meaning)}</td></tr>"
        for option, meaning in tool["options"]
    )
    return (
        '<h2 id="options">Common options</h2>\n<div class="table-wrap"><table>'
        f"<thead><tr><th>Option</th><th>What it does</th></tr></thead><tbody>{rows}</tbody></table></div>"
    )


def render_examples(tool):
    blocks = "".join(
        f"<h3>{escape(title)}</h3>\n<pre><code>{escape(command)}</code></pre>\n"
        for title, command in tool["examples"]
    )
    return f'<h2 id="examples">Examples</h2>\n{blocks}'


def render_sqx_pairings(tool):
    blocks = []
    for title, explanation, command in tool["with_sqx"]:
        explanation_paragraph = f"<p>{escape(explanation)}</p>\n" if explanation else ""
        blocks.append(f"<h3>{escape(title)}</h3>\n{explanation_paragraph}<pre><code>{escape(command)}</code></pre>\n")
    return f'<h2 id="with-sqx">Use {escape(tool["name"])} with sqx</h2>\n{"".join(blocks)}'


def render_tips(tool):
    if not tool["tips"]:
        return ""
    paragraphs = "".join(f"<p>{escape(tip)}</p>" for tip in tool["tips"])
    return f'<div class="callout">{paragraphs}</div>'


def render_learn_more(tool):
    items = "".join(
        f'<li><a href="{escape(url)}" {EXTERNAL_LINK_ATTRIBUTES}>{escape(label)}</a></li>'
        for label, url in tool["links"]
    )
    return (
        '<h2 id="learn-more">Full documentation</h2>\n'
        f"<p>This page covers the essentials. For every option, see the official documentation:</p>\n"
        f'<ul class="external-links">{items}</ul>\n'
        f'<p>Or read the manual in your terminal: <code>man {escape(tool["name"])}</code></p>'
    )


def render_related_tools(tool, base_path, tools):
    related = [other for other in tools if other["category"] == tool["category"] and other["name"] != tool["name"]]
    if not related:
        return ""
    links = "".join(
        f'<a class="tool-chip" href="/{tool_path(base_path, other)}"><code>{escape(other["name"])}</code></a>'
        for other in related
    )
    return f'<h2 id="related">Related tools</h2>\n<div class="tool-chips">{links}</div>'

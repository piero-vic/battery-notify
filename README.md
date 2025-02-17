# `battery-notify`

A lightweight battery notifier daemon. Intended to be used with window managers like i3 or Sway.

It depends on UPower and requires a notification daemon like `mako` to be already installed in your system.

## Installation

```bash
go install .
```

## Usage

Add this line to your sway configuration to start `battery-notify` when starting Sway.

```bash
exec battery-notify
```

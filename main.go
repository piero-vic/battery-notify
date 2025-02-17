package main

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/signal"
	"syscall"

	"github.com/esiqveland/notify"
	"github.com/godbus/dbus/v5"
)

const (
	appName     = "battery-notify"
	batteryPath = dbus.ObjectPath("/org/freedesktop/UPower/devices/battery_BAT0")
)

const (
	stateCharging uint32 = iota + 1
	stateDischarging
	stateEmpty
	stateFullyCharged
	statePendingCharge
	statePendingDischarge
)

var stateMap = map[uint32]string{
	stateCharging:         "Charging",
	stateDischarging:      "Discharging",
	stateEmpty:            "Empty",
	stateFullyCharged:     "Fully Charged",
	statePendingCharge:    "Pending Charge",
	statePendingDischarge: "Pending Discharge",
}

// TODO: Use command line flags to set these
const (
	thresholdCritital = 15
	thresholdLow      = 30
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	sysConn, err := dbus.SystemBus()
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
	defer sysConn.Close()

	sessionConn, err := dbus.SessionBus()
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
	defer sessionConn.Close()

	signalChan := make(chan *dbus.Signal, 10)
	sysConn.Signal(signalChan)

	err = sysConn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.DBus.Properties"),
		dbus.WithMatchObjectPath(batteryPath),
		dbus.WithMatchMember("PropertiesChanged"),
	)
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}

	var lastNotificationID uint32

	slog.Info("Listening for changes in battery")

	for {
		select {
		case <-ctx.Done():
			slog.Info("Quitting")
			os.Exit(1)
		case signal := <-signalChan:
			// Handling signal body format
			if len(signal.Body) < 2 {
				continue
			}
			interfaceName, ok := signal.Body[0].(string)
			if !ok || interfaceName != "org.freedesktop.UPower.Device" {
				continue
			}
			properties, ok := signal.Body[1].(map[string]dbus.Variant)
			if !ok {
				continue
			}

			val, exists := properties["Percentage"]
			if !exists {
				continue
			}
			var percentage float64
			if percentage, ok = val.Value().(float64); !ok {
				continue
			}

			var state uint32
			err := sysConn.Object("org.freedesktop.UPower", signal.Path).
				Call("org.freedesktop.DBus.Properties.Get", 0, "org.freedesktop.UPower.Device", "State").
				Store(&state)
			if err != nil {
				slog.Error(err.Error())
				continue
			}

			var model string
			err = sysConn.Object("org.freedesktop.UPower", signal.Path).
				Call("org.freedesktop.DBus.Properties.Get", 0, "org.freedesktop.UPower.Device", "Model").
				Store(&model)
			if err != nil {
				slog.Error(err.Error())
				continue
			}

			if state != stateDischarging {
				slog.Info(fmt.Sprintf("Skipping notification. State: %s", stateMap[state]))
				continue
			}

			if percentage > thresholdLow {
				slog.Info(fmt.Sprintf("Skipping notification. Battery level: %.0f%%", percentage))
				continue
			}

			notification := notify.Notification{
				AppName:       appName,
				ReplacesID:    lastNotificationID,
				Summary:       fmt.Sprintf("Battery: %s", model),
				Body:          fmt.Sprintf("󰁹 Current level: %.0f%%", percentage),
				ExpireTimeout: notify.ExpireTimeoutSetByNotificationServer,
				Hints: map[string]dbus.Variant{
					"value": dbus.MakeVariant(int(math.Round(percentage))),
				},
			}

			if percentage <= thresholdCritital {
				notification.ExpireTimeout = notify.ExpireTimeoutNever
				notification.SetUrgency(notify.UrgencyCritical)
			} else if percentage <= thresholdLow {
				notification.SetUrgency(notify.UrgencyLow)
			}

			slog.Info("Sending notification")
			lastNotificationID, err = notify.SendNotification(sessionConn, notification)
			if err != nil {
				slog.Error(err.Error())
			}
		}
	}
}

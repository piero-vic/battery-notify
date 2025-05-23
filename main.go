package main

import (
	"context"
	"flag"
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

const (
	dbusUPowerDeviceInterface = "org.freedesktop.UPower.Device"
	dbusCallPropertiesGet     = "org.freedesktop.DBus.Properties.Get"
)

var stateMap = map[uint32]string{
	stateCharging:         "Charging",
	stateDischarging:      "Discharging",
	stateEmpty:            "Empty",
	stateFullyCharged:     "Fully Charged",
	statePendingCharge:    "Pending Charge",
	statePendingDischarge: "Pending Discharge",
}

const usage = `Usage:
  -c, --critical  float  Threshold for critical battery level. Default is 15.
  -l, --low       float  Threshold for low battery level. Default is 30.
`

func main() {
	if err := run(); err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}

func run() error {
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, usage)
	}

	var (
		thresholdCritital float64
		thresholdLow      float64
	)

	flag.Float64Var(&thresholdLow, "l", 30, "Threshold for low battery level.")
	flag.Float64Var(&thresholdLow, "low", 30, "Threshold for low battery level.")
	flag.Float64Var(&thresholdCritital, "c", 15, "Threshold for critical battery level.")
	flag.Float64Var(&thresholdCritital, "critical", 15, "Threshold for critical battery level.")
	flag.Parse()

	if flag.NArg() > 0 {
		flag.Usage()
		return nil
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	sysConn, err := dbus.SystemBus()
	if err != nil {
		return err
	}
	defer sysConn.Close()

	sessionConn, err := dbus.SessionBus()
	if err != nil {
		return err
	}
	defer sessionConn.Close()

	notifier, err := notify.New(sessionConn)
	if err != nil {
		return err
	}

	signalChan := make(chan *dbus.Signal, 10)
	sysConn.Signal(signalChan)

	err = sysConn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.DBus.Properties"),
		dbus.WithMatchObjectPath(batteryPath),
		dbus.WithMatchMember("PropertiesChanged"),
	)
	if err != nil {
		return err
	}

	var lastNotificationID uint32

	slog.Info("Listening for changes in battery")

	for {
		select {
		case <-ctx.Done():
			slog.Info("Quitting")
			return nil
		case signal := <-signalChan:
			// Handling signal body format
			if len(signal.Body) < 2 {
				continue
			}
			properties, ok := signal.Body[1].(map[string]dbus.Variant)
			if !ok {
				continue
			}

			if stateProp, exists := properties["State"]; exists {
				if state, ok := stateProp.Value().(uint32); ok && state == stateCharging {
					slog.Info("Closing last notification")
					_, err := notifier.CloseNotification(lastNotificationID)
					if err != nil {
						slog.Error(err.Error())
					}
				}
			}

			percentageProp, exists := properties["Percentage"]
			if !exists {
				continue
			}
			var percentage float64
			if percentage, ok = percentageProp.Value().(float64); !ok {
				continue
			}

			obj := sysConn.Object("org.freedesktop.UPower", signal.Path)

			var state uint32
			if err := obj.Call(dbusCallPropertiesGet, 0, dbusUPowerDeviceInterface, "State").Store(&state); err != nil {
				slog.Error(err.Error())
				continue
			}

			var model string
			if err := obj.Call(dbusCallPropertiesGet, 0, dbusUPowerDeviceInterface, "Model").Store(&model); err != nil {
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
			lastNotificationID, err = notifier.SendNotification(notification)
			if err != nil {
				slog.Error(err.Error())
			}
		}
	}
}

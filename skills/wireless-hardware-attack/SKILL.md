---
name: wireless-hardware-attack
description: >-
  Wireless/hardware: WiFi PMKID/WPS/Evil Twin, BLE, Zigbee, NFC, SDR, UART/JTAG/SPI, side channels, and fault injection. Use when attacking WiFi, BLE, RFID, SDR, or hardware interfaces.
metadata:
  tags: [penetration-testing, penetration-testing, red-team]
---


## Wireless / Hardware Attacks

```
=== Wireless/hardware ===
WiFi: aircrack-ng/PMKID (hcxdumptool+hashcat)/WPS reaver/Evil Twin | BLE: enumerate GATT with gatttool/unauthenticated reads and writes/Just Works
Zigbee killerbee | clone NFC/RFID with proxmark3/MIFARE mfoc | replay LoRa/SDR with rtl-sdr+gnuradio
Hardware: scan UART baud rates to obtain a shell | read/write firmware through JTAG/SWD | SPI flash dump | DPA/timing side channels | fault injection (voltage/clock glitch to skip authentication) | binwalk -Me
```

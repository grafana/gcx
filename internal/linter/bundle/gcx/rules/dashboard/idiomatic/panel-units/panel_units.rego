# METADATA
# description: Panels should use valid units.
# related_resources:
#  - ref: https://github.com/grafana/gcx/blob/main/docs/reference/linter-rules/dashboard/panel-units.md
#    description: documentation
# custom:
#  severity: warning
package gcx.rules.dashboard.idiomatic["panel-units"]

import data.gcx.result
import data.gcx.utils

# Dashboard v1
report contains violation if {
	utils.resource_is_dashboard_v1(input)
    panels := utils.dashboard_v1_panels(input)
	invalid_panels := [panels[i] | not valid_units[panels[i].fieldConfig.defaults.unit]]

	some i
	invalid_panels[i]

	violation := result.fail(rego.metadata.chain(), sprintf("panel %d uses invalid unit '%s'", [invalid_panels[i].id, invalid_panels[i].fieldConfig.defaults.unit]))
}

# Dashboard v2
report contains violation if {
	utils.resource_is_dashboard_v2(input)

    panels := utils.dashboard_v2_panels(input)
	invalid_panels := [panels[i] | not valid_units[panels[i].object.spec.vizConfig.spec.fieldConfig.defaults.unit]]

	some i
	invalid_panels[i]

	violation := result.fail(rego.metadata.chain(), sprintf("panel '%s' uses invalid unit '%s'", [panels[i].id, panels[i].object.spec.vizConfig.spec.fieldConfig.defaults.unit]))
}

# See https://github.com/grafana/grafana/blob/d0d707895333ddbfbfe4208f8fc8bf65bf0e86e6/packages/grafana-data/src/valueFormats/categories.ts
valid_units := {
    "none",
    "string",
    "short",
    # Misc: SI short
    "sishort",
    # Misc: Percent (0-100)
    "percent",
    # Misc: Percent (0.0-1.0)
    "percentunit",
    # Misc: Humidity (%H)
    "humidity",
    # Misc: Decibel (dB)
    "dB",
    # Misc: Candela (cd)
    "candela",
    # Misc: Hexadecimal (0x)
    "hex0x",
    "hex",
    "sci",
    "locale",
    # Misc: Pixels (px)
    "pixel",

    # Acceleration: Meters/sec² (m/sec²)
    "accMS2",
    # Acceleration: Feet/sec² (f/sec²)
    "accFS2",
    # G unit (g)
    "accG",

    # Angle: Degrees (°)
    "degree",
    # Angle: Radians
    "radian",
    # Angle: Gradian
    "grad",
    # Angle: Arc Minutes
    "arcmin",
    # Angle: Arc Seconds
    "arcsec",

    # Area: Square Meters (m²)
    "areaM2",
    # Area: Square Feet (ft²)
    "areaF2",
    # Area: Square Miles (mi²)
    "areaMI2",
    # Area: Acres (ac)
    "acres",
    # Area: Hectares (ha)
    "hectares",

    # Computation: FLOP/s
    "flops",
    # Computation: MFLOP/s
    "mflops",
    # Computation: GFLOP/s
    "gflops",
    # Computation: TFLOP/s
    "tflops",
    # Computation: PFLOP/s
    "pflops",
    # Computation: EFLOP/s
    "eflops",
    # Computation: ZFLOP/s
    "zflops",
    # Computation: YFLOP/s
    "yflops",

    # Concentration: parts-per-million (ppm)
    "ppm",
    # Concentration: parts-per-billion (ppb)
    "conppb",
    # Concentration: nanogram per cubic meter (ng/m³)
    "conngm3",
    # Concentration: nanogram per normal cubic meter (ng/Nm³)
    "conngNm3",
    # Concentration: microgram per cubic meter (μg/m³)
    "conμgm3",
    # Concentration: microgram per normal cubic meter (μg/Nm³)
    "conμgNm3",
    # Concentration: milligram per cubic meter (mg/m³)
    "conmgm3",
    # Concentration: milligram per normal cubic meter (mg/Nm³)
    "conmgNm3",
    # Concentration: gram per cubic meter (g/m³)
    "congm3",
    # Concentration: gram per normal cubic meter (g/Nm³)
    "congNm3",
    # Concentration: milligrams per decilitre (mg/dL)
    "conmgdL",
    # Concentration: millimoles per litre (mmol/L)
    "conmmolL",

    # Currency: Dollars ($)
    "currencyUSD",
    # Currency: Pounds (£)
    "currencyGBP",
    # Currency: Euro (€)
    "currencyEUR",
    # Currency: Yen (¥)
    "currencyJPY",
    # Currency: Rubles (₽)
    "currencyRUB",
    # Currency: Hryvnias (₴)
    "currencyUAH",
    # Currency: Real (R$)
    "currencyBRL",
    # Currency: Danish Krone (kr)
    "currencyDKK",
    # Currency: Icelandic Króna (kr)
    "currencyISK",
    # Currency: Norwegian Krone (kr)
    "currencyNOK",
    # Currency: Swedish Krona (kr)
    "currencySEK",
    # Currency: Czech koruna (czk)
    "currencyCZK",
    # Currency: Swiss franc (CHF)
    "currencyCHF",
    # Currency: Polish Złoty (PLN)
    "currencyPLN",
    # Currency: Bitcoin (฿)
    "currencyBTC",
    # Currency: Milli Bitcoin (฿)
    "currencymBTC",
    # Currency: Micro Bitcoin (฿)
    "currencyμBTC",
    # Currency: South African Rand (R)
    "currencyZAR",
    # Currency: Indian Rupee (₹)
    "currencyINR",
    # Currency: South Korean Won (₩)
    "currencyKRW",
    # Currency: Indonesian Rupiah (Rp)
    "currencyIDR",
    # Currency: Philippine Peso (PHP)
    "currencyPHP",
    # Currency: Vietnamese Dong (VND)
    "currencyVND",
    # Currency: Turkish Lira (₺)
    "currencyTRY",
    # Currency: Malaysian Ringgit (RM)
    "currencyMYR",
    # Currency: CFP franc (XPF)
    "currencyXPF",
    # Currency: Bulgarian Lev (BGN)
    "currencyBGN",
    # Currency: Guaraní (₲)
    "currencyPYG",
    # Currency: Uruguay Peso (UYU)
    "currencyUYU",
    # Currency: Israeli New Shekels (₪)
    "currencyILS",

    # Data: bytes(IEC)
    "bytes",
    # Data: bytes(SI)
    "decbytes",
    # Data: bits(IEC)
    "bits",
    # Data: bits(SI)
    "decbits",
    # Data: kibibytes
    "kbytes",
    # Data: kilobytes
    "deckbytes",
    # Data: mebibytes
    "mbytes",
    # Data: megabytes
    "decmbytes",
    # Data: gibibytes
    "gbytes",
    # Data: gigabytes
    "decgbytes",
    # Data: tebibytes
    "tbytes",
    # Data: terabytes
    "dectbytes",
    # Data: pebibytes
    "pbytes",
    # Data: petabytes
    "decpbytes",

    # Data rate: packets/sec
    "pps",
    # Data rate: bytes/sec(IEC)
    "binBps",
    # Data rate: bytes/sec(SI)
    "Bps",
    # Data rate: bits/sec(IEC)
    "binbps",
    # Data rate: bits/sec(SI)
    "bps",
    # Data rate: kibibytes/sec
    "KiBs",
    # Data rate: kibibits/sec
    "Kibits",
    # Data rate: kilobytes/sec
    "KBs",
    # Data rate: kilobits/sec
    "Kbits",
    # Data rate: mebibytes/sec
    "MiBs",
    # Data rate: mebibits/sec
    "Mibits",
    # Data rate: megabytes/sec
    "MBs",
    # Data rate: megabits/sec
    "Mbits",
    # Data rate: gibibytes/sec
    "GiBs",
    # Data rate: gibibits/sec
    "Gibits",
    # Data rate: gigabytes/sec
    "GBs",
    # Data rate: gigabits/sec
    "Gbits",
    # Data rate: tebibytes/sec
    "TiBs",
    # Data rate: tebibits/sec
    "Tibits",
    # Data rate: terabytes/sec
    "TBs",
    # Data rate: terabits/sec
    "Tbits",
    # Data rate: pebibytes/sec
    "PiBs",
    # Data rate: pebibits/sec
    "Pibits",
    # Data rate: petabytes/sec
    "PBs",
    # Data rate: petabits/sec
    "Pbits",

    # Date & time: Datetime ISO
    "dateTimeAsIso",
    # Date & time: Datetime ISO (No date if today)
    "dateTimeAsIsoNoDateIfToday",
    # Date & time: Datetime US
    "dateTimeAsUS",
    # Date & time: Datetime US (No date if today)
    "dateTimeAsUSNoDateIfToday",
    # Date & time: Datetime local
    "dateTimeAsLocal",
    # Date & time: Datetime local (No date if today)
    "dateTimeAsLocalNoDateIfToday",
    # Date & time: Datetime default
    "dateTimeAsSystem",
    # Date & time: From Now
    "dateTimeFromNow",

    # Energy: Watt (W)
    "watt",
    # Energy: Kilowatt (kW)
    "kwatt",
    # Energy: Megawatt (MW)
    "megwatt",
    # Energy: Gigawatt (GW)
    "gwatt",
    # Energy: Milliwatt (mW)
    "mwatt",
    # Energy: Watt per square meter (W/m²)
    "Wm2",
    # Energy: Volt-Ampere (VA)
    "voltamp",
    # Energy: Kilovolt-Ampere (kVA)
    "kvoltamp",
    # Energy: Volt-Ampere reactive (VAr)
    "voltampreact",
    # Energy: Kilovolt-Ampere reactive (kVAr)
    "kvoltampreact",
    # Energy: Watt-hour (Wh)
    "watth",
    # Energy: Watt-hour per Kilogram (Wh/kg)
    "watthperkg",
    # Energy: Kilowatt-hour (kWh)
    "kwatth",
    # Energy: Kilowatt-min (kWm)
    "kwattm",
    # Energy: Megawatt-hour (MWh)
    "mwatth",
    # Energy: Ampere-hour (Ah)
    "amph",
    # Energy: Kiloampere-hour (kAh)
    "kamph",
    # Energy: Milliampere-hour (mAh)
    "mamph",
    # Energy: Joule (J)
    "joule",
    # Energy: Electron volt (eV)
    "ev",
    # Energy: Ampere (A)
    "amp",
    # Energy: Kiloampere (kA)
    "kamp",
    # Energy: Milliampere (mA)
    "mamp",
    # Energy: Volt (V)
    "volt",
    # Energy: Kilovolt (kV)
    "kvolt",
    # Energy: Millivolt (mV)
    "mvolt",
    # Energy: Decibel-milliwatt (dBm)
    "dBm",
    # Energy: Milliohm (mΩ)
    "mohm",
    # Energy: Ohm (Ω)
    "ohm",
    # Energy: Kiloohm (kΩ)
    "kohm",
    # Energy: Megaohm (MΩ)
    "Mohm",
    # Energy: Farad (F)
    "farad",
    # Energy: Microfarad (µF)
    "µfarad",
    # Energy: Nanofarad (nF)
    "nfarad",
    # Energy: Picofarad (pF)
    "pfarad",
    # Energy: Femtofarad (fF)
    "ffarad",
    # Energy: Henry (H)
    "henry",
    # Energy: Millihenry (mH)
    "mhenry",
    # Energy: Microhenry (µH)
    "µhenry",
    # Energy: Lumens (Lm)
    "lumens",

    # Flow: Gallons/min (gpm)
    "flowgpm",
    # Flow: Cubic meters/sec (cms)
    "flowcms",
    # Flow: Cubic feet/sec (cfs)
    "flowcfs",
    # Flow: Cubic feet/min (cfm)
    "flowcfm",
    # Flow: Litre/hour
    "litreh",
    # Flow: Litre/min (L/min)
    "flowlpm",
    # Flow: milliLitre/min (mL/min)
    "flowmlpm",
    # Flow: Lux (lx)
    "lux",

    # Force: Newton-meters (Nm)
    "forceNm",
    # Force: Kilonewton-meters (kNm)
    "forcekNm",
    # Force: Newtons (N)
    "forceN",
    # Force: Kilonewtons (kN)
    "forcekN",

    # Hash rate: hashes/sec
    "Hs",
    # Hash rate: kilohashes/sec
    "KHs",
    # Hash rate: megahashes/sec
    "MHs",
    # Hash rate: gigahashes/sec
    "GHs",
    # Hash rate: terahashes/sec
    "THs",
    # Hash rate: petahashes/sec
    "PHs",
    # Hash rate: exahashes/sec
    "EHs",

    # Mass: milligram (mg)
    "massmg",
    # Mass: gram (g)
    "massg",
    # Mass: pound (lb)
    "masslb",
    # Mass: kilogram (kg)
    "masskg",
    # Mass: metric ton (t)
    "masst",

    # Length: millimeter (mm)
    "lengthmm",
    # Length: inch (in)
    "lengthin",
    # Length: feet (ft)
    "lengthft",
    # Length: meter (m)
    "lengthm",
    # Length: kilometer (km)
    "lengthkm",
    # Length: mile (mi)
    "lengthmi",

    # Pressure: Millibars
    "pressurembar",
    # Pressure: Bars
    "pressurebar",
    # Pressure: Kilobars
    "pressurekbar",
    # Pressure: Pascals
    "pressurepa",
    # Pressure: Hectopascals
    "pressurehpa",
    # Pressure: Kilopascals
    "pressurekpa",
    # Pressure: Inches of mercury
    "pressurehg",
    # Pressure: PSI
    "pressurepsi",

    # Radiation: Becquerel (Bq)
    "radbq",
    # Radiation: curie (Ci)
    "radci",
    # Radiation: Gray (Gy)
    "radgy",
    # Radiation: rad
    "radrad",
    # Radiation: Sievert (Sv)
    "radsv",
    # Radiation: milliSievert (mSv)
    "radmsv",
    # Radiation: microSievert (µSv)
    "radusv",
    # Radiation: rem
    "radrem",
    # Radiation: Exposure (C/kg)
    "radexpckg",
    # Radiation: roentgen (R)
    "radr",
    # Radiation: Sievert/hour (Sv/h)
    "radsvh",
    # Radiation: milliSievert/hour (mSv/h)
    "radmsvh",
    # Radiation: microSievert/hour (µSv/h)
    "radusvh",

    # Rotational Speed: Revolutions per minute (rpm)
    "rotrpm",
    # Rotational Speed: Hertz (Hz)
    "rothz",
    # Rotational Speed: Kilohertz (kHz)
    "rotkhz",
    # Rotational Speed: Megahertz (MHz)
    "rotmhz",
    # Rotational Speed: Gigahertz (GHz)
    "rotghz",
    # Rotational Speed: Radians per second (rad/s)
    "rotrads",
    # Rotational Speed: Degrees per second (°/s)
    "rotdegs",

    # Temperature: Celsius (°C)
    "celsius",
    # Temperature: Fahrenheit (°F)
    "fahrenheit",
    # Temperature: Kelvin (K)
    "kelvin",

    # Time: Hertz (1/s)
    "hertz",
    # Time: nanoseconds (ns)
    "ns",
    # Time: microseconds (µs)
    "µs",
    # Time: milliseconds (ms)
    "ms",
    # Time: seconds (s)
    "s",
    # Time: minutes (m)
    "m",
    # Time: hours (h)
    "h",
    # Time: days (d)
    "d",
    # Time: duration (ms)
    "dtdurationms",
    # Time: duration (s)
    "dtdurations",
    # Time: duration (hh:mm:ss)
    "dthms",
    # Time: duration (d hh:mm:ss)
    "dtdhms",
    # Time: Timeticks (s/100)
    "timeticks",
    # Time: clock (ms)
    "clockms",
    # Time: clock (s)
    "clocks",

    # Throughput: counts/sec (cps)
    "cps",
    # Throughput: ops/sec (ops)
    "ops",
    # Throughput: requests/sec (rps)
    "reqps",
    # Throughput: reads/sec (rps)
    "rps",
    # Throughput: writes/sec (wps)
    "wps",
    # Throughput: I/O ops/sec (iops)
    "iops",
    # Throughput: events/sec (eps)
    "eps",
    # Throughput: messages/sec (mps)
    "mps",
    # Throughput: records/sec (rps)
    "recps",
    # Throughput: rows/sec (rps)
    "rowsps",
    # Throughput: counts/min (cpm)
    "cpm",
    # Throughput: ops/min (opm)
    "opm",
    # Throughput: requests/min (rpm)
    "reqpm",
    # Throughput: reads/min (rpm)
    "rpm",
    # Throughput: writes/min (wpm)
    "wpm",
    # Throughput: events/min (epm)
    "epm",
    # Throughput: messages/min (mpm)
    "mpm",
    # Throughput: records/min (rpm)
    "recpm",
    # Throughput: rows/min (rpm)
    "rowspm",

    # Velocity: meters/second (m/s)
    "velocityms",
    # Velocity: kilometers/hour (km/h)
    "velocitykmh",
    # Velocity: miles/hour (mph)
    "velocitymph",
    # Velocity: knot (kn)
    "velocityknot",

    # Volume: millilitre (mL)
    "mlitre",
    # Volume: litre (L)
    "litre",
    # Volume: cubic meter
    "m3",
    # Volume: Normal cubic meter
    "Nm3",
    # Volume: cubic decimeter
    "dm3",
    # Volume: gallons
    "gallons",

    # Boolean: True / False
    "bool",
    # Boolean: Yes / No
    "bool_yes_no",
    # Boolean: On / Off
    "bool_on_off",
}

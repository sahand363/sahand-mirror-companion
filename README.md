# Sahand Mirror Companion (smc)

همراهِ [sahand-mirror](https://github.com/sahand363/sahand-mirror) روی سرورهای مقصد. با یک دستور از خود هاب نصب می‌شود و یک منوی SSH و یک پنل وب فارسی می‌دهد:

- **شبیه‌ساز اینترنت ملی**: با یک کلید همهٔ ارتباط‌های خروجی سرور قطع می‌شود جز هاب. SSH و پنل کار می‌کنند و بعد از زمان تعیین‌شده (پیش‌فرض ۳۰ دقیقه) خودکار وصل می‌شود.
- **تست دسترسی**: GitHub، Google، Docker Hub، PyPI، Ubuntu، Let's Encrypt، Cloudflare، Telegram و خود هاب، با تیک سبز یا ضربدر قرمز.
- **بررسی لیست دانلود**: فهرست برنامه‌هایی که روی هاب آینه شده‌اند، با برچسب «جدید»، «به‌روز شد» و «از هاب حذف شد» نسبت به بررسی قبلی، دستور نصب یک‌خطی، دکمهٔ نصب روی همین سرور و README هر برنامه.

## نصب

روی سرور مقصد (با root). دستور را از داشبورد هاب کپی کنید؛ آدرس هاب داخلش هست:

```bash
bash <(curl -Ls http://HUB-IP:PORT/companion)
```

نصب از خود هاب انجام می‌شود، پس در قطعی هم کار می‌کند (هاب باید نسخهٔ 0.3 یا بالاتر باشد و `sahand-mirror-companion` را آینه کرده باشد؛ از 0.3 خودکار اضافه می‌شود).

از گیت‌هاب، با آدرس هاب:

```bash
bash <(curl -Ls https://raw.githubusercontent.com/sahand363/sahand-mirror-companion/main/install.sh) --hub http://HUB-IP:PORT
```

گزینه‌ها: `--hub`، `--key` (کلید دسترسی هاب)، `--port` (پیش‌فرض 8687)، `--version`.

نصب‌کننده آدرس پنل، نام کاربری و رمز تصادفی را چاپ می‌کند و منو را باز می‌کند.

## منوی SSH

با دستور `smc`:

```
 1  Show UI address and login
 2  Set UI username / password
 3  Change UI port
 4  Simulate national internet (on/off)
 5  Connectivity test
 6  Change hub address / access key
 7  Restart service
 8  Update
 9  Uninstall
 0  Exit
```

بالای منو آدرس پنل، هاب، وضعیت شبیه‌ساز و سرویس نمایش داده می‌شود.

دستورهای مستقیم: `smc sim on 20`، `smc sim off`، `smc sim status`، `smc test`، `smc hub http://IP:PORT [KEY]`، `smc passwd`، `smc update`، `smc uninstall`.

## شبیه‌ساز اینترنت ملی چطور کار می‌کند

یک زنجیرهٔ `SMC_SIM` در `iptables` (و `ip6tables`) جلوی زنجیرهٔ OUTPUT قرار می‌گیرد:

1. loopback مجاز
2. پاسخ اتصال‌های برقرار (ESTABLISHED,RELATED)، پس SSH و پنل قطع نمی‌شوند
3. آی‌پی هاب روی پورت هاب
4. آدرس‌های مجاز اضافه از تنظیمات (مثلاً میرور apt داخلی)
5. DNS روی پورت 53 (قابل خاموش کردن)
6. بقیه REJECT

- زمان‌سنج برگشت با `systemd-run` ساخته می‌شود، پس حتی اگر SSH قطع شود اینترنت برمی‌گردد. ریبوت هم قوانین را پاک می‌کند.
- REJECT خطای فوری می‌دهد؛ در قطعی واقعی بسته‌ها معمولاً DROP می‌شوند و هر تلاش تا timeout صبر می‌کند.
- ترافیک کانتینرهای داکر از زنجیرهٔ FORWARD می‌گذرد و شامل شبیه‌ساز نیست.
- نیازمند `iptables` است (`apt install iptables`).

## مسیرها

- باینری: `/usr/local/bin/sahand-mirror-companion` (و لینک `smc`)
- تنظیمات: `/etc/sahand-mirror-companion/config.json`
- داده: `/var/lib/sahand-mirror-companion`
- سرویس: `sahand-mirror-companion.service`

## API هاب که استفاده می‌شود

- `GET /catalog`: فهرست برنامه‌ها، نسخه، دستور نصب و آمادگی آفلاین
- `GET /catalog/<id>/readme`: README مخزن اصلی برنامه از سورس آینه‌شده
- `GET /companion`: نصب‌کنندهٔ همین برنامه از روی هاب
- `GET /myip`: برای تست دسترسی به هاب

اگر هاب کلید دسترسی داشته باشد، با هدر `X-Mirror-Key` فرستاده می‌شود.

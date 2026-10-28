<div align="center">

<img src="cmd/remotesupport/assets/logo.png" alt="NexDesk" width="200"/>

# NexDesk

### Uzaktan destek, daha kolay.

Windows, Android ve tarayıcı (iPhone dahil) için **açık kaynaklı, şifreli ve her zaman kullanıcı onaylı** uzak masaüstü destek uygulaması.
Reklam yok, hesap yok, lisans ya da süre sınırı yok.

<br/>

![Platform](https://img.shields.io/badge/platform-Windows-0A66C2?style=for-the-badge&logo=windows&logoColor=white)
![Android](https://img.shields.io/badge/teknisyen-Android-3DDC84?style=for-the-badge&logo=android&logoColor=white)
![WebRTC](https://img.shields.io/badge/WebRTC-P2P-333333?style=for-the-badge&logo=webrtc&logoColor=white)
![Lisans](https://img.shields.io/badge/lisans-MIT-26D07C?style=for-the-badge)
![Kurulum](https://img.shields.io/badge/kurulum-tek%20.exe-2E90FF?style=for-the-badge)

</div>

---

## İçindekiler

- [NexDesk nedir?](#nexdesk-nedir)
- [İndirme](#indirme)
- [İlk bağlantı: adım adım](#ilk-bağlantı-adım-adım)
- [Bağlantıyı kesmek](#bağlantıyı-kesmek)
- [Oturum içi araçlar](#oturum-içi-araçlar)
- [Aynı bilgisayara birden fazla teknisyen](#aynı-bilgisayara-birden-fazla-teknisyen)
- [Bilgisayar kaydetme (cihaz erişimi)](#bilgisayar-kaydetme-cihaz-erişimi)
- [Android teknisyen uygulaması](#android-teknisyen-uygulaması)
- [Tarayıcı / iPhone](#tarayıcı--iphone)
- [Bağlantı yolları](#bağlantı-yolları)
- [Sorun giderme](#sorun-giderme)
- [Güvenlik](#güvenlik)
- [Ayarlar ve yerel veriler](#ayarlar-ve-yerel-veriler)
- [Kurumsal kullanım: marka ve kendi sunucunuz](#kurumsal-kullanım-marka-ve-kendi-sunucunuz)
- [Geliştiriciler için](#geliştiriciler-için)

---

## NexDesk nedir?

Birine uzaktan yardım etmek çoğu zaman şöyle başlar: *"Şu programı indir, hesap aç, şu izni ver…"*
NexDesk bunu kısaltır:

> **Destek alan kodu verir → destek veren kodu yazar → destek alan "İzin Ver" der → bağlanırsınız.**

Görüntü, klavye-fare ve dosyalar iki cihaz arasında **uçtan uca şifreli** (WebRTC/DTLS) akar. Doğrudan bağlantı kurulamayan ağlarda trafik, içeriğini göremeyen bir aktarma sunucusundan (TURN) şifreli olarak geçer.

| | Destek **alma** | Destek **verme** | Kurulum | Güncelleme |
|---|:---:|:---:|---|---|
| **Windows** (`NexDesk.exe`) | ✔ | ✔ | Yok — tek dosya | Uygulama içinden, imzalı |
| **Android** (`NexDesk.apk`) | — | ✔ | APK bir kez elle | Uygulama içinden, imzalı |
| **Tarayıcı / iPhone** | — | ✔ | Yok — "Ana Ekrana Ekle" | Sunucuyla birlikte |

Hepsi aynı oturuma birlikte katılabilir: bir teknisyen masadan, biri telefondan, biri iPhone'dan.

---

## İndirme

[**Releases**](../../releases/latest) sayfasındaki son sürümden:

| Dosya | Nedir? |
|---|---|
| `NexDesk.exe` | Windows uygulaması. Kurulum, .NET ya da ek paket gerekmez; çift tıklayın. |
| `NexDesk.apk` | Android teknisyen uygulaması. Bir kez elle kurulur (Android "bilinmeyen kaynak" izni ister). |
| `*.sig` | Güncelleme imzaları; uygulamalar kendileri kullanır, indirmeniz gerekmez. |
| `nexdesk-signaling` | Kendi sunucusunu kurmak isteyenler için Linux sunucu programı. |

> **Windows SmartScreen uyarısı:** Exe kod imzalama sertifikasıyla imzalı olmadığı için ilk açılışta "Windows bilgisayarınızı korudu" uyarısı çıkabilir. **Ek bilgi → Yine de çalıştır** ile açılır.

İlk kurulumdan sonra güncellemeler uygulamanın içinden gelir: **Hakkında → Güncellemeleri Kontrol Et**. Windows sürümü açılışta ve 12 saatte bir, Android sürümü açılışta ve 6 saatte bir kendiliğinden de bakar. Oturum sürerken güncelleme yapılmaz.

---

## İlk bağlantı: adım adım

<table>
<tr>
<td width="50%" valign="top">

### Destek ALAN (yardım isteyen)
1. `NexDesk.exe`'yi açın, **Destek Al**'a tıklayın.
2. İlk seferde Windows Güvenlik Duvarı izni sorulur (yönetici onayı). Bu izin yalnızca yerel ağ içindir.
3. Ekranda çıkan **10 haneli kodu** destek verene iletin (**Kopyala** düğmesi ya da karekod).
4. Bağlantı isteği gelince pencerede bağlananın **adını ve IP adresini** görürsünüz. Tanıyorsanız **Evet / İzin Ver**.
5. Ekranınız paylaşıldığı sürece bunu belirten bir uyarı görünür. İstediğiniz an **Bağlantıyı Kes**.

</td>
<td width="50%" valign="top">

### Destek VEREN (teknisyen)
1. **Destek Ver**'e tıklayın.
2. Karşı tarafın **kodunu** (ya da linkini / internet kodunu) yapıştırın veya **Kayıtlı Cihazlar** listesinden seçin.
3. **Bağlan**'a basın ve karşı tarafın onayını bekleyin.
4. Bağlanınca karşı ekran açılır. Araçlar **İşlemler ▾** menüsündedir.
5. İşiniz bitince **Bağlantıyı Kes**.

</td>
</tr>
</table>

> **Güvenlik kodu (SAS):** Bağlandıktan sonra **İşlemler ▾ → Güvenlik Kodunu Doğrula** ile iki ekrandaki 6 haneli kodu (telefonla) karşılaştırın. Aynıysa araya kimse girmemiştir; farklıysa oturum hemen kesilir.

> **Aynı anda birden fazla kişiye destek:** Sol menüde **＋ Yeni Oturum** ikinci (üçüncü…) bir pencere açar. Her pencere ayrı bir bağlantı yürütür ve başlığında bağlı olduğu kodu gösterir.

Kod, onaylanana kadar **5 dakika** geçerlidir; oturum sürdükçe başka teknisyenler aynı kodla katılabilir (her biri ayrıca onaylanır).

---

## Bağlantıyı kesmek

Bağlantı iki şekilde biter ve NexDesk ikisini birbirinden ayırır:

| Ne oldu? | Karşı tarafta ne olur? |
|---|---|
| Bir taraf **Bağlantıyı Kes**'e bastı, pencereyi **kapattı** (✕), oturumu karşı taraf **Teknisyenler ▾ → Kes** ile sonlandırdı ya da güvenlik kodu uyuşmadı | Karşı taraf bunu **hemen** öğrenir: *"Karşı taraf bağlantıyı kesti — oturum sona erdi."* Kimse boşuna beklemez; yeniden bağlanma denenmez, ana ekrana dönülür. |
| Ağ koptu, bilgisayar uykuya geçti, uygulama çöktü | Bir kesinti sayılır. Teknisyen tarafı birkaç kez **otomatik yeniden bağlanmayı** dener; yarım kalan dosya aktarımı bağlantı geri gelince **kaldığı yerden** sürer. |

- Birden fazla teknisyen bağlıysa, bağlantıyı kesen teknisyen yalnızca kendisi ayrılır; oturum diğerleri için sürer. Son teknisyen ayrılınca destek alan taraftaki oturum da kapanır.
- Destek alan taraf **Bağlantıyı Kes** derse bağlı **bütün** teknisyenlerin bağlantısı kapanır.
- Pencereyi kapatmak yerine **simge durumuna küçültürseniz** bağlantı sürer.

---

## Oturum içi araçlar

Teknisyen penceresinde; çoğu **İşlemler ▾** menüsündedir:

| Araç | Açıklama |
|---|---|
| **Dosya Gönder / sürükle-bırak** | Dosyayı pencereye bırakın. Karşı taraf reddedebilir; aktarım iptal edilebilir, kopmada kaldığı yerden sürer. Alınan dosyalar **İndirilenler** klasörüne gider. |
| **Panoyu Gönder / Al** | Metin panosu iki yönlü. |
| **Panoyu Güvenli Gönder** | Karşıda pano geçmişine yazılmaz, 30 sn sonra silinir (parola vb. için). |
| **Sohbet** | Oturum boyunca yazışma. |
| **Monitör seçimi** | Karşı taraftaki ekranlar arasında geçiş (birden fazla monitör varsa düğmeler çıkar). |
| **Onarım Paketi** | DNS önbelleği, geçici dosyalar, yazıcı kuyruğu, Gezgin, winsock, sfc. Bazıları karşıda yönetici yetkisi ister. |
| **Uzak Sistem Bilgisi** | Bilgisayar, bellek, disk özeti. |
| **Ekran Görüntüsü (F12)** | İndirilenler klasörüne kaydeder. |
| **İşaretleme Modu** | Karşı ekrana çizerek yol gösterin; çizimler solar. |
| **Oturumu Video Kaydet** | `Videolar\NexDesk\*.avi`. Karşı tarafa "KAYIT ALINIYOR" gösterilir. |
| **Makrolar** | İşlemlerinizi kaydedin, sonra tek tıkla tekrar oynatın. |
| **Ekranımı Karşı Tarafa Göster** | Sizin ekranınız karşıda ayrı pencerede, yalnızca izleme. |
| **Oturumu Devret** | Karşı taraf onaylarsa yeni kod üretilir; yeni teknisyen bağlanınca sizin oturumunuz kapanır. |
| **Bu Cihazı Kalıcı Kaydet** | Karşı taraf onaylarsa sonraki bağlantılarda kod ve onay gerekmez ([ayrıntı](#bilgisayar-kaydetme-cihaz-erişimi)). |
| **Bu Bilgisayara Not Ekle** | Not geçmişi; aynı bilgisayara bir sonraki bağlanışta gösterilir. |
| **Gizlilik Perdesi / Giriş Kilidi** | Karşı ekranı karartın / karşıdaki fare-klavyeyi geçici kilitleyin. |
| **Bağlantı Günlüğü** | Bağlantı, kesinti, ekran ve kalite olayları ([sorun giderme](#sorun-giderme)). |

Ayrıca: teknisyen karşı tarafın imlecini görür, destek alan da teknisyenin nereye tıkladığını halkayla görür. Ağ tıkandığında görüntü kalitesi kendiliğinden düşer (720p → 480p → gri ton), açılınca geri yükselir.

### Bağlantılar (adres defteri)
Sol menüdeki **Bağlantılar** sayfası bağlandığınız her bilgisayarı kaydeder: ada, etikete ya da bilgisayar adına göre arama, etiketler (birim, kat, konum…), tarihli not geçmişi ve **Uyandır (Wake-on-LAN)** — o bilgisayar bir kez destek aldığında MAC adresi öğrenilir, aynı ağdaki kapalı makine açılabilir.

---

## Aynı bilgisayara birden fazla teknisyen

Birkaç destek personelinin aynı anda müdahale etmesi gereken durumlar için **lisans, süre ya da kişi sınırı yoktur** (sunucu kötüye kullanıma karşı oturum başına 32 kişilik bir tavan tutar).

- **Kodlu oturumda** ikinci, üçüncü teknisyen aynı kodla katılır; destek alan her biri için ayrıca onay verir.
- **Kayıtlı cihazda** parola yeterlidir; bağlanan her teknisyen ekranda bildirilir ve denetim kaydına yazılır.
- Destek alan tarafta **Teknisyenler (N) ▾** menüsü: ad, adres, bağlı kalma süresi, son müdahale; her satırda **Kes**, altta **Tümünün bağlantısını kes**.
- Herkes kontrol edebilir. Fare/klavye el değiştirdiğinde **"Kontrol: Ahmet"** herkese gösterilir; iki kişi farkında olmadan aynı anda yazmaz.
- Teknisyenler oturumda başka kimlerin olduğunu görür. Her teknisyenin görüntü kalitesi kendi hattına göre ayarlanır; monitör seçimi ortaktır.

---

## Bilgisayar kaydetme (cihaz erişimi)

Sık destek verdiğiniz bilgisayarları bir kez kaydedin; sonraki bağlantılarda kod sormadan **kimlik + parola** ile bağlanın. Tamamen **rızaya dayalıdır**: bilgisayarın kullanıcısı açıkça onaylar, istediği an kapatır; gizli ya da sessiz erişim yoktur.

### En kolay yol: oturum açıkken kaydet
Kodla bağlıyken kaydetme isteği gönderin. Karşı tarafta bir onay kutusu çıkar; kabul edilirse o bilgisayarda cihaz erişimi açılır ve kimlik + parola şifreli bağlantı üzerinden **sizin kayıtlı cihazlarınıza** eklenir.

| Teknisyen uygulaması | Nerede? |
|---|---|
| **Windows** | **İşlemler ▾ → Bu Cihazı Kalıcı Kaydet** |
| **Android** | Üst çubukta **ⓘ → Bu bilgisayarı kaydet** |
| **Tarayıcı / iPhone** | Üst çubukta **ⓘ → Bu bilgisayarı kaydet** |

### Elle kurulum
1. **Destek alınacak bilgisayarda:** Sol menü → **Cihaz Erişimi** → **cihaz kimliği** (boş bırakılırsa bilgisayar adı + rastgele ek, ör. `MUHASEBE-7KQ2`) ve **parola** (en az 4 karakter) → **Kaydet ve Etkinleştir**. Bilgisayar arka planda sunucuya kayıtlı kalır ve normal kullanılmaya devam eder. Parola sonradan değiştirilebilir.
2. **Teknisyen tarafında:**
   - **Windows:** **Destek Ver** → kimlik + parola → **Bağlan**. İlk bağlantıda cihaz **Kayıtlı Cihazlar** listesine eklenir; **Yönet ▾** ile yeniden adlandırılır ya da silinir.
   - **Android ve tarayıcı / iPhone:** Ana ekran → **Kayıtlı cihazlar → ＋** → kimlik, ad, parola.

Kayıtlı cihaza bağlanırken onay sorulmaz (parola yeterlidir); bağlanan her teknisyen bilgisayarda bildirilir ve denetim kaydına yazılır.

- Kapatmak için: **Cihaz Erişimi → Kapalı → Kaydet**. Bağlı teknisyenler **Teknisyenler ▾** menüsünden tek tek de çıkarılabilir.
- Bir Windows oturumunda cihazı yalnızca **bir** NexDesk penceresi duyurur; ikinci pencere teknisyen modunda açılır.
- Elle girilen kimlik başka bir bilgisayara aitse ekranda kırmızı uyarı çıkar.
- Kayıtlı cihaz listesi her uygulamada ayrıdır: Windows'ta kaydettiğiniz bilgisayar telefona kendiliğinden gelmez.

| Parola nerede tutulur? | Nasıl? |
|---|---|
| Sunucu | Düz metin değil: rastgele tuz + SHA-256 özeti. |
| Windows teknisyen | `%APPDATA%\RemoteSupport\tech-devices.json` (yalnızca o bilgisayarda). |
| Android | Android Keystore anahtarıyla AES-GCM şifreli. |
| Tarayıcı / iPhone | Tarayıcıda, dışarı çıkarılamayan bir AES-GCM anahtarıyla şifreli. |

---

## Android teknisyen uygulaması

Telefondan destek verin — nöbette, sahada, masadan uzaktayken.

- **Bağlanma:** destek kodu, **karekod okutma** (uygulama kamera izni istemez), `nexdesk://join…` linkleri ya da **kayıtlı cihazlar**.
- **Dokunmatik kontrol:** dokun = tık · iki kez dokun = çift tık · uzun bas = sağ tık · sürükle = fareyle sürükleme · iki parmak = yakınlaştır / kaydır.
- **Klavye:** yazılanlar karakter olarak gider; Türkçe karakterler karşı tarafın klavye düzeninden bağımsız doğru çıkar. Özel tuşlar: Esc, Tab, Win, oklar, Ctrl+C/V/Z/A, Alt+Tab, F5, Ctrl+Alt+Del, kaydırma.
- **Oturum:** monitör seçimi, sohbet, "Oturumda: …" / "Kontrol: …" göstergeleri, **ⓘ bağlantı bilgisi** (doğrudan/aktarmalı yol, gecikme, kare ve veri hızı, güvenlik kodu) ve **Bu bilgisayarı kaydet**.
- **Bağlantıyı kes:** üst çubuktaki ✕ ya da geri tuşu. Karşı taraf bunu hemen görür.

---

## Tarayıcı / iPhone

Kurulum gerekmeden **iPhone'dan** ve her tarayıcıdan destek verin. Uygulama sunucunun içindedir: `https://<sunucu>/`.

- **iPhone:** Safari'de adresi açın → Paylaş → **Ana Ekrana Ekle**. Tam ekran, simgeli bir uygulama gibi açılır.
- **Bilgisayar tarayıcısı:** fare, tekerlek ve gerçek klavye desteklenir.
- **Telefon:** Android uygulamasıyla aynı dokunmatik hareketler, özel tuşlar, monitör seçimi, sohbet, bağlantı bilgisi ve güvenlik kodu.
- `https://<sunucu>/#code=1234567890` biçimindeki linkler doğrudan o oturuma bağlar.

---

## Bağlantı yolları

Hangi yol kullanılırsa kullanılsın kod **aynı kutuya** yapıştırılır; NexDesk türünü kendisi anlar.

| Yol | Ne zaman? | Örnek | Nasıl çalışır? |
|---|---|---|---|
| **Sunucu üzerinden** (varsayılan) | Her yer, her ağ | `591 490 5011` | Sunucu iki tarafı tanıştırır; doğrudan bağlantı olmazsa trafik şifreli olarak aktarılır. |
| **Cihaz erişimi** | Kayıtlı bilgisayarlar | kimlik + parola | Kod ve onay gerekmez. |
| **Yerel ağ kodu** | Aynı ağ, sunucuya ulaşılamıyorsa | `591 490 5011` | NexDesk'in içindeki tanıştırma servisi + yerel ağ keşfi. |
| **İnternet kodu** (varsayılan kapalı) | Sunucu yokken, ev modemi | `REQKE-2GZKC-0FZX1-S32WG` | Modemde port UPnP ile açılır, dış IP koda gömülür. |
| **Davet kodu** | Sunucu yok, UPnP yok | `DAVET-…` / `YANIT-…` | Tamamen sunucusuz, iki adımlı el sıkışma. |

**Davet kodu:** Destek veren **Destek Ver → Sunucusuz Bağlan (davet kodu)** ile bir kod üretip gönderir → destek alan **Destek Al → Davet Kodunu Yapıştır**, onaylar ve çıkan **YANIT** kodunu geri gönderir → destek veren yanıtı yapıştırır, bağlantı kurulur.

**İnternet kodu:** Sunucu varken gerekmez ve destek alan bilgisayarın 8091 portunu internete açar; bu yüzden kapalıdır. Gerekirse `%APPDATA%\RemoteSupport\settings.json` dosyasına `"internet_code": true` ekleyin. Modemde UPnP kapalıysa 8091/TCP elle yönlendirilmelidir; operatör paylaşımlı IP (CGNAT) kullanıyorsa bu yol çalışmaz, davet kodunu kullanın.

---

## Sorun giderme

İlk bakılacak yer **Bağlantı Günlüğü**dür: teknisyen penceresinde **İşlemler ▾ → Bağlantı Günlüğü**, ya da dosya olarak `%APPDATA%\RemoteSupport\netlog.log`. Destek alan bilgisayardaki günlük, ekranla ilgili sorunlarda en çok bilgiyi verir.

### Bağlanıyor ama ekran gelmiyor
Sohbet, dosya ve monitör seçimi çalışıyor ama görüntü yoksa sorun ağda değil, **destek alan bilgisayarın ekranı yakalayamamasındadır**. NexDesk bunu teknisyene ve günlüğe nedeniyle yazar:

| Teknisyenin gördüğü mesaj | Anlamı ve çözüm |
|---|---|
| *Karşı tarafta Ctrl+Alt+Del / UAC / kilit ekranı açık* | Windows güvenli ekranındayken görüntü verilmez. Kullanıcı kapatınca görüntü kendiliğinden gelir. |
| *NexDesk bilgisayarın ekrandaki oturumunda çalışmıyor* | NexDesk ekrana bağlı olmayan bir Windows oturumunda açılmış: biri o bilgisayara **Uzak Masaüstü (RDP)** ile bağlanıp bırakmış, **kullanıcı değiştirilmiş** ya da program **hizmet / görev zamanlayıcı** ile başlatılmış. NexDesk'i ekrandaki kullanıcı oturumunda açın. |
| *Windows ekran görüntüsü vermiyor (…)* | Çoğunlukla bir **güvenlik / veri sızıntısı önleme (DLP)** yazılımı ekran görüntüsünü engelliyordur. `NexDesk.exe` için istisna tanımlayın. |

Günlükte görülecek satırlar:
- `Ekran yakalanamadı (monitör 1): … [oturum=2 konsol=1 masaüstü=…]` — hatanın kendisi ve tanı. `oturum` ile `konsol` farklıysa NexDesk ekrandaki oturumda değildir.
- `İlk ekran karesi gönderildi: monitör 1, 1920x1080, 210 KB` — görüntü bilgisayardan çıktı demektir; yine de gelmiyorsa ağ tarafına bakın.

### Hiç bağlanamıyor
NexDesk bağlanamazsa adresleri, denenen yolları ve olası sebebi (çok katı NAT, UDP engeli…) ekranda ve günlükte açıklar.
- Kurumsal ağlarda doğrudan bağlantı çoğu zaman kurulamaz; trafik sunucudaki aktarma (TURN) üzerinden gider. Bunun için sunucunun **UDP/TCP 3478** ve **UDP 49160–49200** portlarına çıkış izni gerekir.
- Bağlantı yolu **İşlemler ▾ → Bağlantı Günlüğü**'nde `yol=direct` (doğrudan) ya da `yol=relay` (aktarmalı) olarak görünür.

### "Bu uygulama bilgisayarınızda çalışamıyor"
Exe 64 bit Windows içindir (Windows 10/11 önerilir). 32 bit Windows'ta çalışmaz.

---

## Güvenlik

NexDesk, kötüye kullanılması zor bir destek aracı olacak şekilde tasarlandı:

- **Sessiz bağlantı yok.** Her oturum karşı tarafın açık onayıyla başlar. Cihaz erişimi bile tek seferlik açık bir onay ister ve her an kapatılabilir.
- **Onay penceresi bilgi verir.** Destek alan, onaylamadan önce bağlananın adını ve IP adresini görür. Ad karşı tarafın beyanıdır; şüphede kalırsanız telefonla doğrulayın.
- **Görünür durum.** Ekranı paylaşılan tarafta kalıcı uyarı, kayıt sırasında "KAYIT ALINIYOR" işareti vardır.
- **Uçtan uca şifreleme ve SAS.** Görüntü, kontrol ve dosyalar WebRTC (DTLS) ile şifrelenir; güvenlik kodu karşılaştırması araya girilmesine karşı korur.
- **Dolandırıcılık kalkanı.** Oturum sırasında banka, e-Devlet, kripto borsası ya da ödeme sayfası öne gelirse görüntü ve kontrol durur; kullanıcıya *"sizi arayan kişi kendini banka/polis olarak tanıttıysa bu dolandırıcılık olabilir"* uyarısıyla bağlantıyı kesme seçeneği sunulur (varsayılan: **kes**).
- **Rol koruması.** Destek alan taraf, teknisyenin bilgisayarını yönetemez ya da kilitleyemez.
- **Güvenli masaüstüne saygı.** Ctrl+Alt+Del, UAC ve kilit ekranında görüntü verilmez; kullanıcı kapatınca devam eder.
- **Sunucu kimliği doğrulanır.** Uygulama yalnızca sertifika parmak izi (pin) eşleşen sunucuyla konuşur.
- **İmzalı güncellemeler.** Her güncelleme Ed25519 ile imzalıdır; imza doğrulanmadan hiçbir dosya kurulmaz. GitHub hesabı ele geçirilse bile sahte güncelleme dağıtılamaz.
- **Boşta kalma koruması** (isteğe bağlı otomatik kesme) ve yerel **denetim kaydı** (bağlantı, onay, kalıcı erişim, onarım, kayıt olayları).

---

## Ayarlar ve yerel veriler

- **Ayarlar:** sunucu adresi, TURN, görüntü kalitesi ve çözünürlük, boşta kalma, cihaz erişimi.
- **Hakkında:** tema (**Gece Mavisi · Sıcak · Aydınlık**) ve **Güncellemeleri Kontrol Et**.

Tüm veriler `%APPDATA%\RemoteSupport\` altında, yalnızca o bilgisayarda tutulur:

| Dosya | İçerik |
|---|---|
| `settings.json` | Ayarlar |
| `tech-devices.json` | Teknisyen tarafındaki kayıtlı cihazlar |
| `addressbook.json` | Adres defteri, etiketler, notlar, MAC adresleri |
| `recent.json` | Son bağlantılar |
| `netlog.log` | Bağlantı günlüğü (2 MB'da döner) |
| `audit.log` | Denetim kaydı |
| `macros\*.json` | Kayıtlı makrolar |
| `crash.log` | Yakalanan hatalar |

Video kayıtları `Videolar\NexDesk\`, alınan dosyalar ve ekran görüntüleri `İndirilenler` klasörüne gider.

---

## Kurumsal kullanım: marka ve kendi sunucunuz

### Beyaz etiket
NexDesk'i kendi kurumunuzun adıyla dağıtabilirsiniz. `%APPDATA%\RemoteSupport\` klasörüne bir `branding.json` bırakın:

```json
{
  "name": "Firma Destek",
  "tagline": "Bir tık uzağınızdayız",
  "author": "Firma A.Ş.",
  "accent": "2E90FF",
  "logo_path": "C:\\firma\\logo.png"
}
```

`name` uygulama adı, `tagline` slogan, `author` alt bilgideki yazar, `accent` vurgu rengi (6 haneli HEX), `logo_path` PNG logo yoludur.

### Kendi sunucunuz (signaling + TURN)
Sunucu iki tarafı tanıştırır ve doğrudan bağlantı kurulamayan ağlarda şifreli trafiği aktarır; oturum içeriğini göremez. Küçük bir Ubuntu sunucu yeterlidir (ör. Oracle Cloud ücretsiz katmanı).

1. Ubuntu 22.04/24.04 bir sunucu açın. Gelen kurallar: **TCP 443** (tarayıcı uygulaması), **TCP 8443**, **UDP/TCP 3478**, **UDP 49160–49200**.
2. Sürümdeki `nexdesk-signaling` dosyasını ve depodaki `sunucu/sunucu-kur.sh` betiğini sunucuya kopyalayıp kurun:
   ```bash
   scp -i anahtar.key sunucu-kur.sh nexdesk-signaling ubuntu@SUNUCU_IP:~/
   ssh -i anahtar.key ubuntu@SUNUCU_IP "sed -i 's/\r$//' sunucu-kur.sh && sudo bash sunucu-kur.sh"
   ```
   Betik coturn'ü (TURN) ve signaling servisini kurar, port kontrolü yapar, güvenlik duvarı kurallarını ekler. Tarayıcı uygulaması için sertifikayı Let's Encrypt'ten kendisi alır ve yeniler (kendi alan adınız için `WEB_DOMAIN=destek.kurum.tr`).
3. Betiğin sonunda yazdırdığı `wss://…#pin=…` adresini NexDesk'te **Ayarlar → İnternet Signaling** alanına girin (TURN alanları boş kalır; her onaylı oturuma 12 saat geçerli ayrı bir TURN kimliği verilir). Birden fazla adres virgülle girilebilir; ulaşılabilen ilki kullanılır.

İsteğe bağlı sunucu ayarları (`/etc/nexdesk/signaling.env`): `REMOTESUPPORT_LATEST_APP` istemcilere en son sürümü bildirir, `REMOTESUPPORT_MIN_APP` bundan eski istemcilere "güncelleyin" uyarısı gösterir.

---

## Geliştiriciler için

Gerekenler: **Go 1.26+**; Android için ayrıca **Android Studio** (SDK + NDK) ve Java 17.

```powershell
# Bir kez: güncelleme imza anahtarı (%USERPROFILE%\.nexdesk\update.key — YEDEKLEYİN)
go run ./cmd/nd-sign keygen -pub shared/update/update-pubkey.txt

go test ./...                                                    # testler
powershell -ExecutionPolicy Bypass -File .\build.ps1             # yalnızca dist\NexDesk.exe
powershell -ExecutionPolicy Bypass -File .\release.ps1           # exe + APK + Linux sunucu, hepsi imzalı (-NoAndroid, -Publish)
```

- Sürüm numarası tek yerdedir: `cmd/remotesupport/branding.go` → `appVersion` (Android sürümü de buradan okunur).
- Sürüm GitHub'da `vX.Y.Z` etiketiyle, *pre-release* işaretlenmeden yayımlanmalıdır; uygulamalar yalnızca `releases/latest`'i okur.
- `%USERPROFILE%\.nexdesk\` içindeki `update.key`, `android-release.jks` ve `keystore.properties` dosyalarını **yedekleyin**; kaybolurlarsa kurulu uygulamalar sonraki sürümleri kabul etmez.
- Exe'ye varsayılan sunucu gömmek için `cmd/remotesupport/assets/server-defaults.local.json` (git'e girmez; şablon: `server-defaults.json`).

```
cmd/remotesupport/   Windows uygulaması (Win32 arayüz, oturum, ekran yayını, araçlar)
cmd/signaling/       Linux signaling sunucusu        cmd/nd-sign/  güncelleme imzalama
client/              ekran yakalama, ekran aktarımı, signaling ve WebRTC istemcisi
server/signaling/    signaling mantığı (çoklu teknisyen, cihaz kaydı)
shared/              girdi olayları, karo biçimi, linkler, güncelleme, LAN keşfi
mobile/  android/    Android Go çekirdeği (gomobile) ve Kotlin/Compose uygulaması
web/                 tarayıcı / iPhone uygulaması (sunucuya gömülü)
integration/         uçtan uca testler          sunucu/  sunucu kurulum betiği
```

Ekran 64×64'lük karolara bölünür ve yalnızca değişen karolar gönderilir; durgun ekranda veri gitmez. Yeni kare, ağ öncekini teslim edince üretilir; hat yavaşsa görüntü gecikmez, kare atlanır.

Hata bildirimi ve önerilere açığız — issue açmaktan çekinmeyin.

---

## Lisans

MIT Lisansı. Dilediğiniz gibi kullanın, geliştirin, paylaşın.

<div align="center">

**Program Yazarı: Hasan Güler** · © 2026

</div>

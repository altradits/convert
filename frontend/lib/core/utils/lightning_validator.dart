enum LightningDestinationType {
  bolt11,
  lnurl,
  lightningAddress,
  unknown,
}

class LightningValidator {
  static final RegExp _bolt11Regex = RegExp(
    r'^(lnbc|lntb|lnbcrt|lnsb)[0-9]+[munp]?[a-z0-9]+1[qpzry9x8gf2tvdw0s3jn54khce6mua7l]+$',
    caseSensitive: false,
  );

  static final RegExp _lnurlRegex = RegExp(
    r'^(lnurl1)[0-9a-z]+$',
    caseSensitive: false,
  );

  static final RegExp _lightningAddressRegex = RegExp(
    r'^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$',
  );

  static String cleanDestination(String text) {
    var clean = text.trim();
    if (clean.toLowerCase().startsWith('lightning:')) {
      clean = clean.substring(10);
    }
    return clean;
  }

  static bool isBOLT11(String text) {
    final clean = cleanDestination(text).toLowerCase();
    return _bolt11Regex.hasMatch(clean) ||
        clean.startsWith('lnbc') ||
        clean.startsWith('lntb') ||
        clean.startsWith('lnbcrt') ||
        clean.startsWith('lnsb');
  }

  static bool isLNURL(String text) {
    final clean = cleanDestination(text).toLowerCase();
    return _lnurlRegex.hasMatch(clean) ||
        clean.startsWith('lnurl1') ||
        clean.startsWith('lnurlp://') ||
        clean.startsWith('lnurlw://') ||
        (clean.startsWith('https://') && clean.contains('lnurl'));
  }

  static bool isLightningAddress(String text) {
    final clean = cleanDestination(text);
    return _lightningAddressRegex.hasMatch(clean) &&
        !clean.startsWith('lnbc') &&
        !clean.startsWith('lnurl');
  }

  static bool isValidInvoice(String text) {
    return isValidDestination(text);
  }

  static bool isValidDestination(String text) {
    return isBOLT11(text) || isLNURL(text) || isLightningAddress(text);
  }

  static LightningDestinationType classify(String text) {
    if (isBOLT11(text)) return LightningDestinationType.bolt11;
    if (isLNURL(text)) return LightningDestinationType.lnurl;
    if (isLightningAddress(text)) return LightningDestinationType.lightningAddress;
    return LightningDestinationType.unknown;
  }
}

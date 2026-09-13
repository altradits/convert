class LightningValidator {
  static final RegExp _bolt11Regex = RegExp(
    r'^(lnbc|lntb|lnbcrt)[0-9]+[munp]?[a-z0-9]+1[qpzry9x8gf2tvdw0s3jn54khce6mua7l]+$',
    caseSensitive: false,
  );

  static final RegExp _lnurlRegex = RegExp(
    r'^(lnurl1)[0-9a-z]+$',
    caseSensitive: false,
  );

  static bool isValidInvoice(String text) {
    final clean = text.trim().toLowerCase();
    return _bolt11Regex.hasMatch(clean) || _lnurlRegex.hasMatch(clean);
  }

  static String cleanInvoice(String text) {
    var clean = text.trim();
    if (clean.toLowerCase().startsWith('lightning:')) {
      clean = clean.substring(10);
    }
    return clean;
  }
}

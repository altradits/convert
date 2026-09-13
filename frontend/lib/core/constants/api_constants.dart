class ApiConstants {
  static const String baseUrl = String.fromEnvironment(
    'API_BASE_URL',
    defaultValue: 'http://10.0.2.2:8080', // Android emulator default, use localhost:8080 on iOS/Web
  );

  static const String requestOtp = '$baseUrl/api/v1/auth/request-otp';
  static const String verifyOtp = '$baseUrl/api/v1/auth/verify-otp';
  static const String initiateDeposit = '$baseUrl/api/v1/payments/deposit';
  static const String rates = '$baseUrl/api/v1/rates';

  static String paymentStatus(String txId) => '$baseUrl/api/v1/payments/$txId/status';
}

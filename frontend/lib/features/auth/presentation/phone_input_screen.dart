import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:http/http.dart' as http;
import '../../../core/constants/api_constants.dart';
import '../../deposit/presentation/deposit_screen.dart';

class PhoneInputScreen extends StatefulWidget {
  const PhoneInputScreen({super.key});

  @override
  State<PhoneInputScreen> createState() => _PhoneInputScreenState();
}

class _PhoneInputScreenState extends State<PhoneInputScreen> {
  final _phoneController = TextEditingController();
  final _otpController = TextEditingController();
  final _formKey = GlobalKey<FormState>();
  
  bool _isLoading = false;
  bool _otpSent = false;
  String? _devOtp;

  @override
  void dispose() {
    _phoneController.dispose();
    _otpController.dispose();
    super.dispose();
  }

  void _requestOtp() async {
    if (!_formKey.currentState!.validate()) return;
    setState(() => _isLoading = true);

    final fullPhoneNumber = "+254${_phoneController.text.trim()}";

    try {
      final response = await http.post(
        Uri.parse(ApiConstants.requestOtp),
        headers: {'Content-Type': 'application/json'},
        body: jsonEncode({'phone_number': fullPhoneNumber}),
      ).timeout(const Duration(seconds: 10));

      setState(() => _isLoading = false);

      if (response.statusCode == 200) {
        final data = jsonDecode(response.body);
        _devOtp = data['dev_otp'];
        setState(() => _otpSent = true);
        if (_devOtp != null) {
          _otpController.text = _devOtp!;
        }
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(
              content: Text('OTP sent to $fullPhoneNumber'),
              backgroundColor: Colors.indigo,
            ),
          );
        }
      } else {
        final err = jsonDecode(response.body);
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(
              content: Text(err['error'] ?? 'Failed to send OTP'),
              backgroundColor: Colors.redAccent,
            ),
          );
        }
      }
    } catch (e) {
      setState(() => _isLoading = false);
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('Network error: $e'),
            backgroundColor: Colors.redAccent,
          ),
        );
      }
    }
  }

  void _verifyOtp() async {
    final code = _otpController.text.trim();
    if (code.length != 6) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Please enter the 6-digit OTP code')),
      );
      return;
    }

    setState(() => _isLoading = true);
    final fullPhoneNumber = "+254${_phoneController.text.trim()}";

    try {
      final response = await http.post(
        Uri.parse(ApiConstants.verifyOtp),
        headers: {'Content-Type': 'application/json'},
        body: jsonEncode({'phone_number': fullPhoneNumber, 'code': code}),
      ).timeout(const Duration(seconds: 10));

      setState(() => _isLoading = false);

      if (response.statusCode == 200) {
        if (mounted) {
          Navigator.pushReplacement(
            context,
            MaterialPageRoute(
              builder: (context) => DepositScreen(verifiedPhone: fullPhoneNumber),
            ),
          );
        }
      } else {
        final err = jsonDecode(response.body);
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(
              content: Text(err['error'] ?? 'Invalid verification code'),
              backgroundColor: Colors.redAccent,
            ),
          );
        }
      }
    } catch (e) {
      setState(() => _isLoading = false);
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('Verification error: $e'),
            backgroundColor: Colors.redAccent,
          ),
        );
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Yebente Link Phone', style: TextStyle(fontWeight: FontWeight.bold)),
        centerTitle: true,
      ),
      body: SafeArea(
        child: SingleChildScrollView(
          padding: const EdgeInsets.all(24.0),
          child: Form(
            key: _formKey,
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                const SizedBox(height: 12),
                Center(
                  child: Container(
                    padding: const EdgeInsets.all(16),
                    decoration: BoxDecoration(
                      color: Colors.amber.withOpacity(0.15),
                      shape: BoxShape.circle,
                    ),
                    child: const Icon(Icons.bolt, size: 48, color: Colors.amber),
                  ),
                ),
                const SizedBox(height: 24),
                const Text(
                  'Enter your M-Pesa Number',
                  style: TextStyle(fontSize: 22, fontWeight: FontWeight.bold),
                ),
                const SizedBox(height: 8),
                const Text(
                  'We will send a one-time verification code to authenticate your session.',
                  style: TextStyle(color: Colors.grey, fontSize: 14),
                ),
                const SizedBox(height: 28),
                TextFormField(
                  controller: _phoneController,
                  enabled: !_otpSent,
                  keyboardType: TextInputType.number,
                  inputFormatters: [
                    FilteringTextInputFormatter.digitsOnly,
                    LengthLimitingTextInputFormatter(9),
                  ],
                  decoration: InputDecoration(
                    prefixIcon: Container(
                      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 14),
                      child: const Text(
                        '+254',
                        style: TextStyle(fontSize: 16, fontWeight: FontWeight.bold),
                      ),
                    ),
                    hintText: '707172370',
                    border: OutlineInputBorder(borderRadius: BorderRadius.circular(12)),
                  ),
                  validator: (value) {
                    if (value == null || value.isEmpty) return 'Enter your phone number';
                    if (value.length != 9) return 'Enter exactly 9 digits';
                    if (!['7', '1'].contains(value[0])) return 'Must start with 7 or 1';
                    return null;
                  },
                ),
                if (_otpSent) ...[
                  const SizedBox(height: 20),
                  const Text('Enter 6-Digit OTP Code', style: TextStyle(fontWeight: FontWeight.w600)),
                  const SizedBox(height: 8),
                  TextFormField(
                    controller: _otpController,
                    keyboardType: TextInputType.number,
                    inputFormatters: [
                      FilteringTextInputFormatter.digitsOnly,
                      LengthLimitingTextInputFormatter(6),
                    ],
                    decoration: InputDecoration(
                      prefixIcon: const Icon(Icons.security),
                      hintText: '123456',
                      border: OutlineInputBorder(borderRadius: BorderRadius.circular(12)),
                    ),
                  ),
                ],
                const SizedBox(height: 32),
                SizedBox(
                  width: double.infinity,
                  height: 52,
                  child: ElevatedButton(
                    style: ElevatedButton.styleFrom(
                      backgroundColor: Colors.amber.shade700,
                      foregroundColor: Colors.white,
                      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
                    ),
                    onPressed: _isLoading ? null : (_otpSent ? _verifyOtp : _requestOtp),
                    child: _isLoading
                        ? const CircularProgressIndicator(color: Colors.white)
                        : Text(
                            _otpSent ? 'Verify Code' : 'Send Verification Code',
                            style: const TextStyle(fontSize: 16, fontWeight: FontWeight.bold),
                          ),
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

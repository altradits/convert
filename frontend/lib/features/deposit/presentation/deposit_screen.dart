import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:http/http.dart' as http;
import '../../../core/constants/api_constants.dart';
import '../../../core/utils/lightning_validator.dart';
import 'tx_status_screen.dart';

class DepositScreen extends StatefulWidget {
  final String verifiedPhone;
  const DepositScreen({super.key, required this.verifiedPhone});

  @override
  State<DepositScreen> createState() => _DepositScreenState();
}

class _DepositScreenState extends State<DepositScreen> with WidgetsBindingObserver {
  final _amountController = TextEditingController();
  final _invoiceController = TextEditingController();
  bool _isInvoiceValid = false;
  bool _isSubmitting = false;
  int _estimatedSats = 0;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _checkClipboard();
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _amountController.dispose();
    _invoiceController.dispose();
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) {
      _checkClipboard();
    }
  }

  Future<void> _checkClipboard() async {
    final data = await Clipboard.getData(Clipboard.kTextPlain);
    if (data?.text != null) {
      final text = LightningValidator.cleanInvoice(data!.text!);
      if (LightningValidator.isValidInvoice(text) && text != _invoiceController.text) {
        setState(() {
          _invoiceController.text = text;
          _isInvoiceValid = true;
        });
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            const SnackBar(
              content: Text('Auto-detected Lightning invoice from clipboard!'),
              backgroundColor: Colors.indigo,
            ),
          );
        }
      }
    }
  }

  void _onInvoiceChanged(String val) {
    final clean = LightningValidator.cleanInvoice(val);
    setState(() {
      _isInvoiceValid = LightningValidator.isValidInvoice(clean);
    });
  }

  void _onAmountChanged(String val) {
    final amount = double.tryParse(val) ?? 0;
    setState(() {
      // 8,500,000 KES/BTC rate estimation
      _estimatedSats = ((amount / 8500000.0) * 100000000).round();
    });
  }

  Future<void> _initiateDeposit() async {
    final amount = double.tryParse(_amountController.text.trim());
    if (amount == null || amount <= 0) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Please enter a valid amount')),
      );
      return;
    }

    final invoice = LightningValidator.cleanInvoice(_invoiceController.text.trim());
    if (!LightningValidator.isValidInvoice(invoice)) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Please enter a valid BOLT11 Lightning invoice')),
      );
      return;
    }

    setState(() => _isSubmitting = true);

    try {
      final response = await http.post(
        Uri.parse(ApiConstants.initiateDeposit),
        headers: {'Content-Type': 'application/json'},
        body: jsonEncode({
          'phone_number': widget.verifiedPhone,
          'amount_kes': amount,
          'invoice': invoice,
        }),
      ).timeout(const Duration(seconds: 10));

      setState(() => _isSubmitting = false);

      if (response.statusCode == 200) {
        final data = jsonDecode(response.body);
        final txId = data['transaction_id'];

        if (mounted) {
          Navigator.push(
            context,
            MaterialPageRoute(
              builder: (context) => TransactionStatusScreen(
                phone: widget.verifiedPhone,
                amountKES: amount,
                invoice: invoice,
                txId: txId,
              ),
            ),
          );
        }
      } else {
        final err = jsonDecode(response.body);
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(
              content: Text(err['error'] ?? 'Deposit initiation failed'),
              backgroundColor: Colors.redAccent,
            ),
          );
        }
      }
    } catch (e) {
      setState(() => _isSubmitting = false);
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

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Deposit & Settle', style: TextStyle(fontWeight: FontWeight.bold)),
        centerTitle: true,
      ),
      body: SingleChildScrollView(
        padding: const EdgeInsets.all(24.0),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Container(
              padding: const EdgeInsets.all(16),
              decoration: BoxDecoration(
                color: Colors.amber.withOpacity(0.1),
                borderRadius: BorderRadius.circular(16),
                border: Border.all(color: Colors.amber.withOpacity(0.3)),
              ),
              child: Row(
                children: [
                  const Icon(Icons.phone_android, color: Colors.amber, size: 28),
                  const SizedBox(width: 12),
                  Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      const Text('Linked M-Pesa Number', style: TextStyle(color: Colors.grey, fontSize: 12)),
                      Text(widget.verifiedPhone, style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 16)),
                    ],
                  ),
                ],
              ),
            ),
            const SizedBox(height: 24),
            const Text('Amount to Deposit', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 16)),
            const SizedBox(height: 8),
            TextField(
              controller: _amountController,
              keyboardType: const TextInputType.numberWithOptions(decimal: true),
              onChanged: _onAmountChanged,
              decoration: InputDecoration(
                prefixText: 'KES ',
                prefixStyle: const TextStyle(fontWeight: FontWeight.bold),
                hintText: '500.00',
                border: OutlineInputBorder(borderRadius: BorderRadius.circular(12)),
              ),
            ),
            if (_estimatedSats > 0) ...[
              const SizedBox(height: 8),
              Text(
                '≈ $_estimatedSats sats (Estimate)',
                style: const TextStyle(color: Colors.amber, fontWeight: FontWeight.bold, fontSize: 14),
              ),
            ],
            const SizedBox(height: 24),
            const Text('Destination Lightning Invoice', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 16)),
            const SizedBox(height: 8),
            TextField(
              controller: _invoiceController,
              onChanged: _onInvoiceChanged,
              maxLines: 3,
              decoration: InputDecoration(
                hintText: 'lnbc10u1p...',
                suffixIcon: IconButton(
                  icon: const Icon(Icons.content_paste),
                  tooltip: 'Paste from clipboard',
                  onPressed: _checkClipboard,
                ),
                border: OutlineInputBorder(borderRadius: BorderRadius.circular(12)),
              ),
            ),
            const SizedBox(height: 12),
            if (_isInvoiceValid)
              const Row(
                children: [
                  Icon(Icons.check_circle, color: Colors.green, size: 20),
                  SizedBox(width: 8),
                  Text('Valid Lightning Invoice verified', style: TextStyle(color: Colors.green, fontWeight: FontWeight.w600)),
                ],
              ),
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
                onPressed: (_isInvoiceValid && !_isSubmitting) ? _initiateDeposit : null,
                child: _isSubmitting
                    ? const CircularProgressIndicator(color: Colors.white)
                    : const Text('Initiate M-Pesa STK Push', style: TextStyle(fontSize: 16, fontWeight: FontWeight.bold)),
              ),
            ),
          ],
        ),
      ),
    );
  }
}
